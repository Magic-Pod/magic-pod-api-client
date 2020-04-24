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
	app.Version = "0.50.0.1"
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
				cli.IntFlag{
					Name:  "test_condition_number, c",
					Usage: "Test condition number defined in the project batch run page",
				},
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
		Succeeded 	int
		Failed    	int
		Aborted   	int
		Unresolved	int
		Total     	int
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
	urlBase, apiToken, organization, project, httpHeadersMap, err := ParseCommonFlags(c)
	if err != nil {
		return err
	}
	appPath := c.String("app_path")
	if appPath == "" {
		return cli.NewExitError("--app_path option is required", 1)
	}

	fileNo, exitErr := UploadApp(urlBase, apiToken, organization, project, httpHeadersMap, appPath)
	if exitErr != nil {
		return exitErr
	}
	fmt.Printf("%d\n", fileNo)
	return nil
}

func DeleteAppAction(c *cli.Context) error {
	// handle command line arguments
	urlBase, apiToken, organization, project, httpHeadersMap, err := ParseCommonFlags(c)
	if err != nil {
		return err
	}
	appFileNumber := c.Int("app_file_number")
	if appFileNumber == 0 {
		return cli.NewExitError("--app_file_number option is not specified or 0", 1)
	}
	exitErr := DeleteApp(urlBase, apiToken, organization, project, httpHeadersMap, appFileNumber)
	if exitErr != nil {
		return exitErr
	}
	return nil
}

func BatchRunAction(c *cli.Context) error {
	// handle command line arguments
	urlBase, apiToken, organization, project, httpHeadersMap, err := ParseCommonFlags(c)
	if err != nil {
		return err
	}
	testConditionNumber := c.Int("test_condition_number")
	setting := c.String("setting")
	if testConditionNumber == 0 && setting == "" {
		return cli.NewExitError("Either of --test_condition_number or --setting option is required", 1)
	}
	noWait := c.Bool("no_wait")
	waitLimit := c.Int("wait_limit")

	// send batch run start request
	batchRuns, exitErr := StartBatchRun(urlBase, apiToken, organization, project, httpHeadersMap, testConditionNumber, setting)
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
	existsUnresolved := false
	for _, batchRun := range batchRuns {
		fmt.Printf("\n#%d wait until %d tests to be finished.. \n", batchRun.Batch_Run_Number, batchRun.Test_Cases.Total)
		prevFinished := 0
		for {
			batchRun, exitErr := GetBatchRun(urlBase, apiToken, organization, project, httpHeadersMap, batchRun.Batch_Run_Number)
			if exitErr != nil {
				fmt.Print(exitErr)
				existsErr = true
				break // give up the wait here
			}
			finished := batchRun.Test_Cases.Succeeded + batchRun.Test_Cases.Failed + batchRun.Test_Cases.Aborted + batchRun.Test_Cases.Unresolved
			fmt.Printf(".") // show progress to prevent "long time no output" error on CircleCI etc
			// output progress
			if finished != prevFinished {
				notSuccessfulCount := ""
				if batchRun.Test_Cases.Failed > 0 {
					notSuccessfulCount = fmt.Sprintf("%d failed", batchRun.Test_Cases.Failed)
				}
				if batchRun.Test_Cases.Unresolved > 0 {
					if notSuccessfulCount != "" {
						notSuccessfulCount += ", "
					}
					notSuccessfulCount += fmt.Sprintf("%d unresolved", batchRun.Test_Cases.Unresolved)
				}
				if notSuccessfulCount != "" {
					notSuccessfulCount = fmt.Sprintf(" (%s)", notSuccessfulCount)
				}
				fmt.Printf("%d/%d finished%s\n", finished, batchRun.Test_Cases.Total, notSuccessfulCount)
				prevFinished = finished
			}
			if batchRun.Status != "running" {
				if batchRun.Test_Cases.Unresolved > 0 {
					existsUnresolved = true
				}
				if batchRun.Status == "succeeded" {
					fmt.Print("batch run succeeded\n")
					break
				} else if batchRun.Status == "failed" {
					if batchRun.Test_Cases.Failed > 0 {
						unresolved := ""
						if existsUnresolved {
							unresolved = fmt.Sprintf(", %d unresolved", batchRun.Test_Cases.Unresolved)
						}
						fmt.Printf("batch run failed (%d failed%s)\n", batchRun.Test_Cases.Failed, unresolved)
					} else {
						fmt.Print("batch run failed\n")
					}
					existsErr = true
					break
				} else if batchRun.Status == "unresolved" {
					fmt.Printf("batch run unresolved (%d unresolved)\n", batchRun.Test_Cases.Unresolved)
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
	if existsUnresolved {
		return cli.NewExitError("", 2)
	}
	return nil
}

func MergeTestConditionNumberToSetting(testSettingsMap map[string]interface{}, hasTestSettings bool, testConditionNumber int) string {
	testSettingsMap["test_condition_number"] = testConditionNumber

	if !hasTestSettings {
		// convert {\"model\":\"Nexus 5X\"} to {\"test_settings\":[{\"model\":\"Nexus 5X\"}]}
		// so that it can be treated with test_condition_number
		miscSettings := make(map[string]interface{})
		for k, v := range testSettingsMap {
			if k != "test_condition_number" && k != "concurrency" {
				miscSettings[k] = v
			}
		}
		if len(miscSettings) > 0 {
			settingsArray := [...]map[string]interface{}{miscSettings}
			testSettingsMap["test_settings"] = settingsArray
		}
	}

	settingBytes, _ := json.Marshal(testSettingsMap)
	return string(settingBytes)
}

func StartBatchRun(urlBase string, apiToken string, organization string, project string, httpHeadersMap map[string]string, testConditionNumber int, setting string) ([]BatchRun, *cli.ExitError) {
	var testSettings interface{}
	isCrossBatchRunSetting := (testConditionNumber != 0)
	if setting == "" {
		setting = "{\"test_condition_number\":" + strconv.Itoa(testConditionNumber) + "}"
	} else {
		err := json.Unmarshal([]byte(setting), &testSettings)
		if err == nil {
			testSettingsMap, ok := testSettings.(map[string]interface{})
			if ok {
				_, hasTestSettings := testSettingsMap["test_settings"]
				testConditionNumberInJSON, hasTestConditionNumber := testSettingsMap["test_condition_number"]
				if testConditionNumber != 0 {
					if hasTestConditionNumber && testConditionNumber != testConditionNumberInJSON {
						return []BatchRun{}, cli.NewExitError("--test_condition_number and --setting have different number", 1)
					}
					setting = MergeTestConditionNumberToSetting(testSettingsMap, hasTestSettings, testConditionNumber)
				}
				isCrossBatchRunSetting = isCrossBatchRunSetting || hasTestSettings || hasTestConditionNumber
			}
		}
	}
	if isCrossBatchRunSetting {
		res, err := CreateBaseRequest(urlBase, apiToken, organization, project, httpHeadersMap).
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
		res, err := CreateBaseRequest(urlBase, apiToken, organization, project, httpHeadersMap).
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

func GetBatchRun(urlBase string, apiToken string, organization string, project string, httpHeadersMap map[string]string, batchRunNumber int) (*BatchRun, *cli.ExitError) {
	res, err := CreateBaseRequest(urlBase, apiToken, organization, project, httpHeadersMap).
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

func UploadApp(urlBase string, apiToken string, organization string, project string, httpHeadersMap map[string]string, appPath string) (int, *cli.ExitError) {
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
	res, err := CreateBaseRequest(urlBase, apiToken, organization, project, httpHeadersMap).
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

func DeleteApp(urlBase string, apiToken string, organization string, project string, httpHeadersMap map[string]string, appFileNumber int) *cli.ExitError {
	res, err := CreateBaseRequest(urlBase, apiToken, organization, project, httpHeadersMap).
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
		cli.StringFlag{
			Name:   "http_headers, H",
			Usage:  "Additional HTTP headers in JSON string format",
		},
	}
}

func ParseCommonFlags(c *cli.Context) (string, string, string, string, map[string]string, error) {
	urlBase := c.GlobalString("url-base")
	apiToken := c.String("token")
	organization := c.String("organization")
	project := c.String("project")
	httpHeadersMap := make(map[string]string)
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
		httpHeadersStr := c.String("http_headers")
		if httpHeadersStr != "" {
			err = json.Unmarshal([]byte(httpHeadersStr), &httpHeadersMap)
			if err != nil {
				err = cli.NewExitError("http headers must be in JSON string format whose keys and values are string", 1)
			}
		}
	}
	return urlBase, apiToken, organization, project, httpHeadersMap, err
}

func CreateBaseRequest(urlBase string, apiToken string, organization string, project string, httpHeadersMap map[string]string) *resty.Request {
	client := resty.New()
	return client.
		SetHostURL(urlBase+"/api/v1.0").R().
		SetHeader("Authorization", "Token "+string(apiToken)).
		SetHeaders(httpHeadersMap).
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
