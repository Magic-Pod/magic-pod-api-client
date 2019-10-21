package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mholt/archiver"
	"github.com/urfave/cli"
	"gopkg.in/resty.v1"
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

type BatchRun struct {
	Url              string
	Status           string
	Batch_Run_Number int
	Test_Cases       struct {
		Succeeded int
		Failed    int
		Aborted   int
		Total     int
	}
}

type UploadFile struct {
	File_No   int
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

	fileNo, exitErr := UploadApp(urlBase, apiToken, organization, project, appPath)
	if exitErr != nil {
		return exitErr
	}
	fmt.Printf("%d\n", fileNo)
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
	batchRun, exitErr := StartBatchRun(urlBase, apiToken, organization, project, setting)
	if exitErr != nil {
		return exitErr
	}

	// finish before the test finish
	totalTestCount := batchRun.Test_Cases.Total
	if noWait {
		fmt.Printf("test result page: %s\n", batchRun.Url)
		return nil
	}

	// wait until the batch test is finished
	fmt.Printf("test result page:\n%s\n\n", batchRun.Url)
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
		batchRun, exitErr = GetBatchRun(urlBase, apiToken, organization, project, batchRun.Batch_Run_Number)
		if exitErr != nil {
			return exitErr // give up the wait here
		}
		finished := batchRun.Test_Cases.Succeeded + batchRun.Test_Cases.Failed + batchRun.Test_Cases.Aborted
		// output progress
		if finished != prevFinished {
			if batchRun.Test_Cases.Failed > 0 {
				fmt.Printf("%d/%d finished (%d failed)\n", finished, totalTestCount, batchRun.Test_Cases.Failed)
			} else {
				fmt.Printf("%d/%d finished\n", finished, totalTestCount)
			}
			prevFinished = finished
		}
		if batchRun.Status != "running" {
			if batchRun.Status == "succeeded" {
				fmt.Print("batch run succeeded\n")
				return nil
			} else if batchRun.Status == "failed" {
				return cli.NewExitError(fmt.Sprintf("batch run failed (%d failed)", batchRun.Test_Cases.Failed), 1)
			} else if batchRun.Status == "aborted" {
				return cli.NewExitError("bartch run aborted", 1)
			} else {
				panic(batchRun.Status)
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

func StartBatchRun(urlBase string, apiToken string, organization string, project string, setting string) (*BatchRun, *cli.ExitError) {
	res, err := CreateBaseRequest(urlBase, apiToken, organization, project).
		SetHeader("Content-Type", "application/json").
		SetBody(setting).
		SetResult(BatchRun{}).
		Post("/{organization}/{project}/batch-run/")
	if err != nil {
		panic(err)
	}
	if exitErr := HandleError(res); exitErr != nil {
		return nil, exitErr
	}
	return res.Result().(*BatchRun), nil
}

func GetBatchRun(urlBase string, apiToken string, organization string, project string, batchRunNumber int) (*BatchRun, *cli.ExitError) {
	res, err := CreateBaseRequest(urlBase, apiToken, organization, project).
		SetPathParams(map[string]string{
			"batch_run_number": strconv.Itoa(batchRunNumber),
		}).
		SetResult(BatchRun{}).
		Get("/{organization}/{project}/batch-run/{batch_run_number}/")
	if err != nil {
		panic(err)
	}
	if exitErr := HandleError(res); exitErr != nil {
		return nil, exitErr
	}
	return res.Result().(*BatchRun), nil
}

func UploadApp(urlBase string, apiToken string, organization string, project string, appPath string) (int, *cli.ExitError) {
	stat, err := os.Stat(appPath)
	if err != nil {
		return 0, cli.NewExitError(fmt.Sprintf("%s does not exist", appPath), 1)
	}
	var actualPath string
	if stat.Mode().IsDir() {
		if strings.HasSuffix(appPath, ".app") {
			actualPath = ZipAppDir(appPath)
		} else {
			return 0, cli.NewExitError(fmt.Sprintf("%s is not file but direcoty.", appPath), 1)
		}
	} else {
		actualPath = appPath
	}
	res, err := CreateBaseRequest(urlBase, apiToken, organization, project).
		SetFile("file", actualPath).
		SetResult(UploadFile{}).
		Post("/{organization}/{project}/upload-file/")
	if err != nil {
		panic(err)
	}
	if exitErr := HandleError(res); exitErr != nil {
		return 0, exitErr
	}
	return res.Result().(*UploadFile).File_No, nil
}

func ZipAppDir(dirPath string) string {
	zipPath := dirPath + ".zip"
	if err := os.RemoveAll(zipPath); err != nil {
		panic(err)
	}
	if err := archiver.Archive([]string{dirPath}, zipPath); err != nil {
		panic(err)
	}
	return zipPath
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

func CreateBaseRequest(urlBase string, apiToken string, organization string, project string) *resty.Request {
	return resty.
		SetHostURL(urlBase+"/api/v1.0").R().
		SetHeader("Authorization", "Token "+string(apiToken)).
		SetPathParams(map[string]string{
			"organization": organization,
			"project":      project,
		})
}

func HandleError(resp *resty.Response) *cli.ExitError {
	if resp.StatusCode() != 200 {
		return cli.NewExitError(fmt.Sprintf("%s: %s", resp.Status(), resp.String()), 1)
	} else {
		return nil
	}
}
