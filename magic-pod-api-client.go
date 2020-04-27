package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Magic-Pod/magic-pod-api-client/common"
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
			Flags: append(commonFlags(), []cli.Flag{
				cli.IntFlag{
					Name:  "test_settings_number, S",
					Usage: "Test settings number defined in the project batch run page",
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
			Action: batchRunAction,
		},
		{
			Name:  "upload-app",
			Usage: "Upload app/ipa/apk file",
			Flags: append(commonFlags(), []cli.Flag{
				cli.StringFlag{
					Name:  "app_path, a",
					Usage: "Path to the app/ipa/apk file to upload",
				},
			}...),
			Action: uploadAppAction,
		},
		{
			Name:  "delete-app",
			Usage: "Deleted uploaded app/ipa/apk file",
			Flags: append(commonFlags(), []cli.Flag{
				cli.IntFlag{
					Name:  "app_file_number, a",
					Usage: "File number of the uploaded file",
				},
			}...),
			Action: deleteAppAction,
		},
	}
	app.Run(os.Args)
}

func uploadAppAction(c *cli.Context) error {
	// handle command line arguments
	urlBase, apiToken, organization, project, httpHeadersMap, err := parseCommonFlags(c)
	if err != nil {
		return err
	}
	appPath := c.String("app_path")
	if appPath == "" {
		return cli.NewExitError("--app_path option is required", 1)
	}

	fileNo, exitErr := common.UploadApp(urlBase, apiToken, organization, project, httpHeadersMap, appPath)
	if exitErr != nil {
		return exitErr
	}
	fmt.Printf("%d\n", fileNo)
	return nil
}

func deleteAppAction(c *cli.Context) error {
	// handle command line arguments
	urlBase, apiToken, organization, project, httpHeadersMap, err := parseCommonFlags(c)
	if err != nil {
		return err
	}
	appFileNumber := c.Int("app_file_number")
	if appFileNumber == 0 {
		return cli.NewExitError("--app_file_number option is not specified or 0", 1)
	}
	exitErr := common.DeleteApp(urlBase, apiToken, organization, project, httpHeadersMap, appFileNumber)
	if exitErr != nil {
		return exitErr
	}
	return nil
}

func batchRunAction(c *cli.Context) error {
	// handle command line arguments
	urlBase, apiToken, organization, project, httpHeadersMap, err := parseCommonFlags(c)
	if err != nil {
		return err
	}
	testSettingsNumber := c.Int("test_settings_number")
	setting := c.String("setting")
	if testSettingsNumber == 0 && setting == "" {
		return cli.NewExitError("Either of --test_settings_number or --setting option is required", 1)
	}
	noWait := c.Bool("no_wait")
	waitLimit := c.Int("wait_limit")

	_, existsErr, existsUnresolved, err := common.ExecuteBatchRun(urlBase, apiToken, organization,
		project, httpHeadersMap, testSettingsNumber, setting, !noWait, waitLimit, true)
	if err != nil {
		return err
	}
	if existsErr {
		return cli.NewExitError("", 1)
	}
	if existsUnresolved {
		return cli.NewExitError("", 2)
	}
	return nil
}

func commonFlags() []cli.Flag {
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
			Name:  "http_headers, H",
			Usage: "Additional HTTP headers in JSON string format",
		},
	}
}

func parseCommonFlags(c *cli.Context) (string, string, string, string, map[string]string, error) {
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
