package common

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty"
	"github.com/mholt/archiver"
	"github.com/urfave/cli"
)

// BatchRun stands for a batch run executed on the server
type BatchRun struct {
	Url              string
	Status           string
	Batch_Run_Number int
	Test_Cases       struct {
		Succeeded  int
		Failed     int
		Aborted    int
		Unresolved int
		Total      int
	}
}

// BatchRuns stands for a group of batch runs executed on the server
type BatchRuns struct {
	Batch_Runs []BatchRun
}

// UploadFile stands for a file to be uploaded to the server
type UploadFile struct {
	File_No int
}

func zipAppDir(dirPath string) string {
	zipPath := dirPath + ".zip"
	if err := os.RemoveAll(zipPath); err != nil {
		panic(err)
	}
	if err := archiver.Archive([]string{dirPath}, zipPath); err != nil {
		panic(err)
	}
	return zipPath
}

func createBaseRequest(urlBase string, apiToken string, organization string, project string, httpHeadersMap map[string]string) *resty.Request {
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

func handleError(resp *resty.Response) *cli.ExitError {
	if resp.StatusCode() != 200 {
		return cli.NewExitError(fmt.Sprintf("%s: %s", resp.Status(), resp.String()), 1)
	} else {
		return nil
	}
}

// UploadApp uploads app/ipa/apk file to the server
func UploadApp(urlBase string, apiToken string, organization string, project string, httpHeadersMap map[string]string, appPath string) (int, *cli.ExitError) {
	stat, err := os.Stat(appPath)
	if err != nil {
		return 0, cli.NewExitError(fmt.Sprintf("%s does not exist", appPath), 1)
	}
	var actualPath string
	if stat.Mode().IsDir() {
		if strings.HasSuffix(appPath, ".app") {
			actualPath = zipAppDir(appPath)
		} else {
			return 0, cli.NewExitError(fmt.Sprintf("%s is not file but direcoty.", appPath), 1)
		}
	} else {
		actualPath = appPath
	}
	res, err := createBaseRequest(urlBase, apiToken, organization, project, httpHeadersMap).
		SetFile("file", actualPath).
		SetResult(UploadFile{}).
		Post("/{organization}/{project}/upload-file/")
	if err != nil {
		panic(err)
	}
	if exitErr := handleError(res); exitErr != nil {
		return 0, exitErr
	}
	return res.Result().(*UploadFile).File_No, nil
}

func mergeTestSettingsNumberToSetting(testSettingsMap map[string]interface{}, hasTestSettings bool, testSettingsNumber int) string {
	testSettingsMap["test_settings_number"] = testSettingsNumber

	if !hasTestSettings {
		// convert {\"model\":\"Nexus 5X\"} to {\"test_settings\":[{\"model\":\"Nexus 5X\"}]}
		// so that it can be treated with test_settings_number
		miscSettings := make(map[string]interface{})
		keysToDelete := []string{}
		for k, v := range testSettingsMap {
			if k != "test_settings_number" && k != "concurrency" {
				miscSettings[k] = v
				keysToDelete = append(keysToDelete, k)
			}
		}
		for k := range keysToDelete {
			delete(testSettingsMap, keysToDelete[k])
		}
		if len(miscSettings) > 0 {
			settingsArray := [...]map[string]interface{}{miscSettings}
			testSettingsMap["test_settings"] = settingsArray
		}
	}

	settingBytes, _ := json.Marshal(testSettingsMap)
	return string(settingBytes)
}

// StartBatchRun starts a batch run or a cross batch run on the server
func StartBatchRun(urlBase string, apiToken string, organization string, project string, httpHeadersMap map[string]string, testSettingsNumber int, setting string) ([]BatchRun, *cli.ExitError) {
	var testSettings interface{}
	isCrossBatchRunSetting := (testSettingsNumber != 0)
	if setting == "" {
		setting = "{\"test_settings_number\":" + strconv.Itoa(testSettingsNumber) + "}"
	} else {
		err := json.Unmarshal([]byte(setting), &testSettings)
		if err == nil {
			testSettingsMap, ok := testSettings.(map[string]interface{})
			if ok {
				_, hasTestSettings := testSettingsMap["test_settings"]
				testSettingsNumberInJSON, hasTestSettingsNumber := testSettingsMap["test_settings_number"]
				if testSettingsNumber != 0 {
					if hasTestSettingsNumber && testSettingsNumber != testSettingsNumberInJSON {
						return []BatchRun{}, cli.NewExitError("--test_settings_number and --setting have different number", 1)
					}
					setting = mergeTestSettingsNumberToSetting(testSettingsMap, hasTestSettings, testSettingsNumber)
				}
				isCrossBatchRunSetting = isCrossBatchRunSetting || hasTestSettings || hasTestSettingsNumber
			}
		}
	}
	if isCrossBatchRunSetting {
		res, err := createBaseRequest(urlBase, apiToken, organization, project, httpHeadersMap).
			SetHeader("Content-Type", "application/json").
			SetBody(setting).
			Post("/{organization}/{project}/cross-batch-run/")
		if err != nil {
			panic(err)
		}
		if exitErr := handleError(res); exitErr != nil {
			return []BatchRun{}, exitErr
		}
		// Temporal logic to handle old Magic-Pod API earlier than 0.65.0
		batchRuns := BatchRuns{}
		json.Unmarshal(res.Body(), &batchRuns)
		if len(batchRuns.Batch_Runs) > 0 {
			return batchRuns.Batch_Runs, nil	
		}
		batchRun := BatchRun{}
		json.Unmarshal(res.Body(), &batchRun)
		return []BatchRun{batchRun}, nil
	} else { // normal batch run
		res, err := createBaseRequest(urlBase, apiToken, organization, project, httpHeadersMap).
			SetHeader("Content-Type", "application/json").
			SetBody(setting).
			SetResult(BatchRun{}).
			Post("/{organization}/{project}/batch-run/")
		if err != nil {
			panic(err)
		}
		if exitErr := handleError(res); exitErr != nil {
			return []BatchRun{}, exitErr
		}
		batchRun := res.Result().(*BatchRun)
		return []BatchRun{*batchRun}, nil
	}
}

// GetBatchRun retrieves status and number of test cases executed of a specified batch run
func GetBatchRun(urlBase string, apiToken string, organization string, project string, httpHeadersMap map[string]string, batchRunNumber int) (*BatchRun, *cli.ExitError) {
	res, err := createBaseRequest(urlBase, apiToken, organization, project, httpHeadersMap).
		SetPathParams(map[string]string{
			"batch_run_number": strconv.Itoa(batchRunNumber),
		}).
		SetResult(BatchRun{}).
		Get("/{organization}/{project}/batch-run/{batch_run_number}/")
	if err != nil {
		panic(err)
	}
	if exitErr := handleError(res); exitErr != nil {
		return nil, exitErr
	}
	return res.Result().(*BatchRun), nil
}

func LatestBatchRunNo(urlBase string, apiToken string, organization string, project string, httpHeadersMap map[string]string) (int, *cli.ExitError) {
	res, err := createBaseRequest(urlBase, apiToken, organization, project, httpHeadersMap).
		SetQueryParam("count", "1").
		SetResult(BatchRuns{}).
		Get("/{organization}/{project}/batch-runs/")
	if err != nil {
		panic(err)
	}
	if exitErr := handleError(res); exitErr != nil {
		return 0, exitErr
	}
	batchRuns := res.Result().(*BatchRuns).Batch_Runs
	if len(batchRuns) == 0 {
		return 0, cli.NewExitError("no batch run exists in this project", 1)
	}
	return batchRuns[0].Batch_Run_Number, nil
}

// DeleteApp deletes app/ipa/apk file on the server
func DeleteApp(urlBase string, apiToken string, organization string, project string, httpHeadersMap map[string]string, appFileNumber int) *cli.ExitError {
	res, err := createBaseRequest(urlBase, apiToken, organization, project, httpHeadersMap).
		SetBody(fmt.Sprintf("{\"app_file_number\":%d}", appFileNumber)).
		Delete("/{organization}/{project}/delete-file/")
	if err != nil {
		panic(err)
	}
	if exitErr := handleError(res); exitErr != nil {
		return exitErr
	}
	return nil
}

func GetScreenshots(urlBase string, apiToken string, organization string, project string, httpHeadersMap map[string]string, batchRunNumber int, downloadPath string, fileIndexType string, fileNameBodyType string, downloadType string, maskDynamicallyChangedArea bool) error {
	var maskDynamicallyChangedAreaStr string
	if maskDynamicallyChangedArea {
		maskDynamicallyChangedAreaStr = "true"
	} else {
		maskDynamicallyChangedAreaStr = "false"
	}
	res, err := createBaseRequest(urlBase, apiToken, organization, project, httpHeadersMap).
		SetPathParams(map[string]string{
			"batch_run_number": strconv.Itoa(batchRunNumber),
		}).
		SetQueryParam("file_index_type", fileIndexType).
		SetQueryParam("file_name_body_type", fileNameBodyType).
		SetQueryParam("download_type", downloadType).
		SetQueryParam("mask_dynamically_changed_area", maskDynamicallyChangedAreaStr).
		SetOutput(downloadPath).
		Get("/{organization}/{project}/batch-runs/{batch_run_number}/screenshots/")
	if err != nil {
		panic(err)
	}
	if res.StatusCode() != 200 {
		// response body is included not in res but in downloadPath file,
		responseText, err := ioutil.ReadFile(downloadPath)
		if err != nil {
			panic(err)
		}
		// remove downloadPath since it contains not zip contents but just error information
		err = os.Remove(downloadPath)
		if err != nil {
			panic(err)
		}
		return cli.NewExitError(fmt.Sprintf("%s: %s", res.Status(), responseText), 1)
	}
	return nil
}

func printMessage(printResult bool, format string, args ...interface{}) {
	if printResult {
		fmt.Printf(format, args...)
	}
}

// ExecuteBatchRun starts batch run(s) and wait for its completion with showing progress
func ExecuteBatchRun(urlBase string, apiToken string, organization string, project string,
	httpHeadersMap map[string]string, testSettingsNumber int, setting string,
	waitForResult bool, waitLimit int, printResult bool) (bool, bool, *cli.ExitError) {
	// send batch run start request
	batchRuns, exitErr := StartBatchRun(urlBase, apiToken, organization, project, httpHeadersMap, testSettingsNumber, setting)
	if exitErr != nil {
		return false, false, exitErr
	}

	crossBatchRunTotalTestCount := 0
	printMessage(printResult, "test result page:\n")
	for _, batchRun := range batchRuns {
		printMessage(printResult, "%s\n", batchRun.Url)
		crossBatchRunTotalTestCount += batchRun.Test_Cases.Total
	}

	// finish before the test finish
	if !waitForResult {
		return false, false, nil
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
	for key, batchRun := range batchRuns {
		printMessage(printResult, "\n#%d wait until %d tests to be finished.. \n", batchRun.Batch_Run_Number, batchRun.Test_Cases.Total)
		prevFinished := 0
		for {
			batchRun, exitErr := GetBatchRun(urlBase, apiToken, organization, project, httpHeadersMap, batchRun.Batch_Run_Number)
			if exitErr != nil {
				if printResult {
					fmt.Print(exitErr)
				}
				existsErr = true
				break // give up the wait here
			}
			batchRuns[key] = *batchRun
			finished := batchRun.Test_Cases.Succeeded + batchRun.Test_Cases.Failed + batchRun.Test_Cases.Aborted + batchRun.Test_Cases.Unresolved
			printMessage(printResult, ".") // show progress to prevent "long time no output" error on CircleCI etc
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
				printMessage(printResult, "%d/%d finished%s\n", finished, batchRun.Test_Cases.Total, notSuccessfulCount)
				prevFinished = finished
			}
			if batchRun.Status != "running" {
				if batchRun.Test_Cases.Unresolved > 0 {
					existsUnresolved = true
				}
				if batchRun.Status == "succeeded" {
					printMessage(printResult, "batch run succeeded\n")
					break
				} else if batchRun.Status == "failed" {
					if batchRun.Test_Cases.Failed > 0 {
						unresolved := ""
						if existsUnresolved {
							unresolved = fmt.Sprintf(", %d unresolved", batchRun.Test_Cases.Unresolved)
						}
						printMessage(printResult, "batch run failed (%d failed%s)\n", batchRun.Test_Cases.Failed, unresolved)
					} else {
						printMessage(printResult, "batch run failed\n")
					}
					existsErr = true
					break
				} else if batchRun.Status == "unresolved" {
					printMessage(printResult, "batch run unresolved (%d unresolved)\n", batchRun.Test_Cases.Unresolved)
					break
				} else if batchRun.Status == "aborted" {
					printMessage(printResult, "batch run aborted\n")
					existsErr = true
					break
				} else {
					panic(batchRun.Status)
				}
			}
			if passedSeconds > limitSeconds {
				return existsErr, existsUnresolved, cli.NewExitError("batch run never finished", 1)
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
	return existsErr, existsUnresolved, nil
}
