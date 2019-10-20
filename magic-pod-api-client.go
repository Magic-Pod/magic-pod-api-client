package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/urfave/cli"
)

func main() {
	app := cli.NewApp()
	app.Version = "0.39.0"
	app.Name = "magic-pod-api-client"
	app.Usage = "Simple and useful wrapper for Magic Pod Web API"
	app.Flags = []cli.Flag{
		// hidden option only for Magic Pod developers
		cli.StringFlag{
			Name:   "url-base",
			Value:  "https://magic-pod.com",
			Hidden: true,
		},
	}
	app.Commands = []cli.Command{
		{
			Name:  "batch-run",
			Usage: "Run batch test",
			Flags: append(CommonFlags(), []cli.Flag{
				cli.StringFlag{
					Name:  "setting, s",
					Usage: "Test setting in JSON format",
				},
				cli.BoolFlag{
					Name:  "no_wait, n",
					Usage: "Return immediately without waiting the batch run to be finished",
				},
				cli.IntFlag{
					Name:  "wait_limit, w",
					Usage: "Wait limit in seconds. If 0 is specified, the value is test count x 10 minutes",
				},
			}...),
			Action: BatchRunAction,
		},
		{
			Name:  "upload-app",
			Usage: "Upload app/ipa/apk file",
			Flags: append(CommonFlags(), []cli.Flag{
				cli.StringFlag{
					Name:  "app_path, a",
					Usage: "Path to the app/ipa/apk file to upload",
				},
			}...),
			Action: UploadAppAction,
		},
	}
	app.Run(os.Args)
}

type BatchRunStartRes struct {
	Url              string
	Batch_Run_Number int
	Test_Cases       struct {
		Total int
	}
}

type BatchRunGetRes struct {
	Status     string
	Test_Cases struct {
		Succeeded int
		Failed    int
		Aborted   int
	}
}

// WIP
func UploadAppAction(c *cli.Context) error {
	// handle command line arguments
	urlBase, apiToken, organization, project, err := ParseCommonFlags(c)
	if err != nil {
		return err
	}
	appPath := c.String("app_path")
	if appPath == "" {
		return cli.NewExitError("--app_path option is required", 1)
	}

	url := fmt.Sprintf("%s/api/v1.0/%s/%s/upload-file/", urlBase, organization, project)
	fmt.Printf(url + apiToken)
	return nil
}

func BatchRunAction(c *cli.Context) error {
	// handle command line arguments
	urlBase, apiToken, organization, project, err := ParseCommonFlags(c)
	if err != nil {
		return err
	}
	setting := c.String("setting")
	if setting == "" {
		return cli.NewExitError("--setting option is required", 1)
	}
	noWait := c.Bool("no_wait")
	waitLimit := c.Int("wait_limit")

	// send batch run start request
	url := fmt.Sprintf("%s/api/v1.0/%s/%s/batch-run/", urlBase, organization, project)
	startResBody, exitErr := SendHttpRequest("POST", url, bytes.NewBuffer([]byte(setting)), apiToken, "application/json")
	if exitErr != nil {
		return exitErr
	}
	var startRes BatchRunStartRes
	err = json.Unmarshal(startResBody, &startRes)
	if err != nil {
		panic(err)
	}

	// finish before the test finish
	totalTestCount := startRes.Test_Cases.Total
	if noWait {
		fmt.Printf("test result page: %s\n", startRes.Url)
		return nil
	}

	// wait until the batch test is finished
	fmt.Printf("test result page:\n%s\n\n", startRes.Url)
	fmt.Printf("wait until %d tests to be finished.. \n", totalTestCount)
	const retryInterval = 30
	var limitSeconds int
	if waitLimit == 0 {
		limitSeconds = totalTestCount * retryInterval * 10 // wait up to test count x 10 minutes by default
	} else {
		limitSeconds = waitLimit
	}
	passedSeconds := 0
	prevFinished := 0
	for {
		url := fmt.Sprintf("%s/api/v1.0/%s/%s/batch-run/%d/", urlBase, organization, project, startRes.Batch_Run_Number)
		getResBody, exitErr := SendHttpRequest("GET", url, nil, apiToken, "application/json")
		if exitErr != nil {
			return exitErr // give up the wait here
		}
		var getRes BatchRunGetRes
		err = json.Unmarshal(getResBody, &getRes)
		if err != nil {
			panic(err)
		}
		finished := getRes.Test_Cases.Succeeded + getRes.Test_Cases.Failed + getRes.Test_Cases.Aborted
		// output progress
		if finished != prevFinished {
			if getRes.Test_Cases.Failed > 0 {
				fmt.Printf("%d/%d finished (%d failed)\n", finished, totalTestCount, getRes.Test_Cases.Failed)
			} else {
				fmt.Printf("%d/%d finished\n", finished, totalTestCount)
			}
			prevFinished = finished
		}
		if getRes.Status != "running" {
			if getRes.Status == "succeeded" {
				fmt.Print("batch run succeeded\n")
				return nil
			} else if getRes.Status == "failed" {
				return cli.NewExitError(fmt.Sprintf("batch run failed (%d failed)", getRes.Test_Cases.Failed), 1)
			} else if getRes.Status == "aborted" {
				return cli.NewExitError("bartch run aborted", 1)
			} else {
				panic(getRes.Status)
			}
		}
		if passedSeconds > limitSeconds {
			return cli.NewExitError("batch run never finished", 1)
		}
		time.Sleep(retryInterval * time.Second)
		passedSeconds += retryInterval
	}
	return nil
}

func CommonFlags() []cli.Flag {
	return []cli.Flag{
		cli.StringFlag{
			Name:   "token, t",
			Usage:  "API token. You can get the value from https://magic-pod.com/accounts/api-token/",
			EnvVar: "MAGIC_POD_API_TOKEN",
		},
		cli.StringFlag{
			Name:   "organization, o",
			Usage:  "Organization name. (Not \"organization display name\", be careful!)",
			EnvVar: "MAGIC_POD_ORGANIZATION",
		},
		cli.StringFlag{
			Name:   "project, p",
			Usage:  "Project name. (Not \"project display name\", be careful!)",
			EnvVar: "MAGIC_POD_PROJECT",
		},
	}
}

func ParseCommonFlags(c *cli.Context) (string, string, string, string, error) {
	urlBase := c.GlobalString("url-base")
	apiToken := c.String("token")
	organization := c.String("organization")
	project := c.String("project")
	var err error
	if urlBase == "" {
		err = cli.NewExitError("url-base argument cannot be empty", 1)
	} else if apiToken == "" {
		err = cli.NewExitError("--token option is required", 1)
	} else if organization == "" {
		err = cli.NewExitError("--organization option is required", 1)
	} else if project == "" {
		err = cli.NewExitError("--project option is required", 1)
	} else {
		err = nil
	}
	return urlBase, apiToken, organization, project, err
}

// return: (response body or nil, error)
func SendHttpRequest(method string, url string, body io.Reader, apiToken string, contentType string) ([]byte, *cli.ExitError) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Authorization", "Token "+apiToken)
	req.Header.Set("Content-Type", contentType)
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()
	resBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}
	if res.StatusCode != 200 {
		return nil, cli.NewExitError(fmt.Sprintf("%s: %s", res.Status, resBody), 1)
	}
	return resBody, nil
}
