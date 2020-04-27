package common

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

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

// CrossBatchRun stands for a group of batch runs executed on the server
type CrossBatchRun struct {
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
			SetResult(CrossBatchRun{}).
			Post("/{organization}/{project}/cross-batch-run/")
		if err != nil {
			panic(err)
		}
		if exitErr := handleError(res); exitErr != nil {
			return []BatchRun{}, exitErr
		}
		crossBatchRun := res.Result().(*CrossBatchRun)
		return crossBatchRun.Batch_Runs, nil
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
