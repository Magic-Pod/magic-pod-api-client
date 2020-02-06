package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty"
	"github.com/mholt/archiver"
	"github.com/urfave/cli"
)

func main() {
	app := cli.NewApp()
	app.Version = "0.46.0.1"
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
					Usage: "Test setting in JSON format. Please check https://magic-pod.com/api/v1.0/doc/ for more detail",
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
		{
			Name:  "delete-app",
			Usage: "Deleted uploaded app/ipa/apk file",
			Flags: append(CommonFlags(), []cli.Flag{
				cli.IntFlag{
					Name:  "app_file_number, a",
					Usage: "File number of the uploaded file",
				},
			}...),
			Action: DeleteAppAction,
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

type CrossBatchRun struct {
	Batch_Runs []BatchRun
}

type UploadFile struct {
	File_No int
}

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

func DeleteAppAction(c *cli.Context) error {
	// handle command line arguments
	urlBase, apiToken, organization, project, err := ParseCommonFlags(c)
	if err != nil {
		return err
	}
	appFileNumber := c.Int("app_file_number")
	if appFileNumber == 0 {
		return cli.NewExitError("--app_file_number option is not specified or 0", 1)
	}
	exitErr := DeleteApp(urlBase, apiToken, organization, project, appFileNumber)
	if exitErr != nil {
		return exitErr
	}
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
	batchRuns, exitErr := StartBatchRun(urlBase, apiToken, organization, project, setting)
	if exitErr != nil {
		return exitErr
	}

	crossBatchRunTotalTestCount := 0
	fmt.Print("test result page:\n")
	for _, batchRun := range batchRuns {
		fmt.Printf("%s\n", batchRun.Url)
		crossBatchRunTotalTestCount += batchRun.Test_Cases.Total
	}

	// finish before the test finish
	if noWait {
		return nil
	}

	const initRetryInterval = 10 // retry more frequently at first
	const retryInterval = 60
	var limitSeconds int
	if waitLimit == 0 {
		limitSeconds = crossBatchRunTotalTestCount * 10 * 60 // wait up to test count x 10 minutes by default
	} else {
		limitSeconds = waitLimit
	}
	passedSeconds := 0
	existsErr := false
	for _, batchRun := range batchRuns {
		fmt.Printf("\n#%d wait until %d tests to be finished.. \n", batchRun.Batch_Run_Number, batchRun.Test_Cases.Total)
		prevFinished := 0
		for {
			batchRun, exitErr := GetBatchRun(urlBase, apiToken, organization, project, batchRun.Batch_Run_Number)
			if exitErr != nil {
				fmt.Print(exitErr)
				existsErr = true
				break // give up the wait here
			}
			finished := batchRun.Test_Cases.Succeeded + batchRun.Test_Cases.Failed + batchRun.Test_Cases.Aborted
			fmt.Printf(".") // show progress to prevent "long time no output" error on CircleCI etc
			// output progress
			if finished != prevFinished {
				if batchRun.Test_Cases.Failed > 0 {
					fmt.Printf("%d/%d finished (%d failed)\n", finished, batchRun.Test_Cases.Total, batchRun.Test_Cases.Failed)
				} else {
					fmt.Printf("%d/%d finished\n", finished, batchRun.Test_Cases.Total)
				}
				prevFinished = finished
			}
			if batchRun.Status != "running" {
				if batchRun.Status == "succeeded" {
					fmt.Print("batch run succeeded\n")
					break
				} else if batchRun.Status == "failed" {
					if batchRun.Test_Cases.Failed > 0 {
						fmt.Printf("batch run failed (%d failed)\n", batchRun.Test_Cases.Failed)
					} else {
						fmt.Print("batch run failed\n")
					}
					existsErr = true
					break
				} else if batchRun.Status == "aborted" {
					fmt.Print("batch run aborted\n")
					existsErr = true
					break
				} else {
					panic(batchRun.Status)
				}
			}
			if passedSeconds > limitSeconds {
				return cli.NewExitError("batch run never finished", 1)
			}
			if passedSeconds < 120 {
				time.Sleep(initRetryInterval * time.Second)
				passedSeconds += initRetryInterval
			} else {
				time.Sleep(retryInterval * time.Second)
				passedSeconds += retryInterval
			}
		}
	}
	if existsErr {
		return cli.NewExitError("", 1)
	}
	return nil
}

func StartBatchRun(urlBase string, apiToken string, organization string, project string, setting string) ([]BatchRun, *cli.ExitError) {
	var testSettings interface{}
	err := json.Unmarshal([]byte(setting), &testSettings)
	isCrossBatchRunSetting := false
	if err == nil {
		testSettingsMap, ok := testSettings.(map[string]interface{})
		if ok {
			_, isCrossBatchRunSetting = testSettingsMap["test_settings"]
		}
	}
	if isCrossBatchRunSetting {
		res, err := CreateBaseRequest(urlBase, apiToken, organization, project).
			SetHeader("Content-Type", "application/json").
			SetBody(setting).
			SetResult(CrossBatchRun{}).
			Post("/{organization}/{project}/cross-batch-run/")
		if err != nil {
			panic(err)
		}
		if exitErr := HandleError(res); exitErr != nil {
			return []BatchRun{}, exitErr
		}
		crossBatchRun := res.Result().(*CrossBatchRun)
		return crossBatchRun.Batch_Runs, nil
	} else { // normal batch run
		res, err := CreateBaseRequest(urlBase, apiToken, organization, project).
			SetHeader("Content-Type", "application/json").
			SetBody(setting).
			SetResult(BatchRun{}).
			Post("/{organization}/{project}/batch-run/")
		if err != nil {
			panic(err)
		}
		if exitErr := HandleError(res); exitErr != nil {
			return []BatchRun{}, exitErr
		}
		batchRun := res.Result().(*BatchRun)
		return []BatchRun{*batchRun}, nil
	}
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

func DeleteApp(urlBase string, apiToken string, organization string, project string, appFileNumber int) *cli.ExitError {
	res, err := CreateBaseRequest(urlBase, apiToken, organization, project).
		SetBody(fmt.Sprintf("{\"app_file_number\":%d}", appFileNumber)).
		Delete("/{organization}/{project}/delete-file/")
	if err != nil {
		panic(err)
	}
	if exitErr := HandleError(res); exitErr != nil {
		return exitErr
	}
	return nil
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
	client := resty.New()
	return client.
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
