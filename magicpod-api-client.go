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
	app.Version = "0.40.0"
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
			Flags: []cli.Flag{
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
				cli.StringFlag{
					Name:  "setting, s",
					Usage: "Test setting in JSON format",
				},
			},
			Action: BatchRunAction,
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
	Status string
}

func BatchRunAction(c *cli.Context) error {
	// handle command line arguments
	urlBase := c.GlobalString("url-base")
	if urlBase == "" {
		return cli.NewExitError("url-base argument cannot be empty", 1)
	}
	apiToken := c.String("token")
	if apiToken == "" {
		return cli.NewExitError("--token option is required", 1)
	}
	organization := c.String("organization")
	if organization == "" {
		return cli.NewExitError("--organization option is required", 1)
	}
	project := c.String("project")
	if project == "" {
		return cli.NewExitError("--project option is required", 1)
	}
	setting := c.String("setting")
	if setting == "" {
		return cli.NewExitError("--setting option is required", 1)
	}

	// send batch run start request
	url := fmt.Sprintf("%s/api/v1.0/%s/%s/batch-run/", urlBase, organization, project)
	startResBody, exitErr := SendHttpRequest("POST", url, bytes.NewBuffer([]byte(setting)), apiToken)
	if exitErr != nil {
		return exitErr
	}
	var startRes BatchRunStartRes
	err := json.Unmarshal(startResBody, &startRes)
	if err != nil {
		panic(err)
	}
	fmt.Printf(fmt.Sprintf("%s\n", startResBody))
	fmt.Printf("new batch run has started\n")

	// wait until the batch test is finished
	totalTestCount := startRes.Test_Cases.Total
	fmt.Printf(fmt.Sprintf("wait until %d tests to be finished.. (%s)\n", totalTestCount, startRes.Url))
	retryLimit := totalTestCount * 10 // wait up to test count x 10 minutes by default
	retryCount := 0
	for {
		url := fmt.Sprintf("%s/api/v1.0/%s/%s/batch-run/%d/", urlBase, organization, project, startRes.Batch_Run_Number)
		getResBody, exitErr := SendHttpRequest("GET", url, nil, apiToken)
		if exitErr != nil {
			return exitErr // give up the wait here
		}
		var getRes BatchRunGetRes
		err = json.Unmarshal(getResBody, &getRes)
		if err != nil {
			panic(err)
		}
		if getRes.Status != "running" {
			if getRes.Status == "succeeded" {
				fmt.Print("all tests succeeded\n")
				return nil
			} else if getRes.Status == "failed" {
				return cli.NewExitError("batch run failed", 1)
			} else if getRes.Status == "aborted" {
				return cli.NewExitError("bartch run aborted", 1)
			} else {
				panic(getRes.Status)
			}
		}
		retryCount += 1
		if retryCount >= retryLimit {
			return cli.NewExitError("The batch run never finished", 1)
		}
		time.Sleep(60 * time.Second)
	}
	return nil
}

// returrn: (response body or nil, error)
func SendHttpRequest(method string, url string, body io.Reader, apiToken string) ([]byte, *cli.ExitError) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Authorization", "Token "+apiToken)
	req.Header.Set("accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
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
