# magic-pod-api-client
Simple and useful wrapper for Magic Pod Web API.

## Install

Download the latest magic-pod-api-client executable from [here](https://github.com/Magic-Pod/magic-pod-api-client/releases) and unzip it to any directory you prefer.

## Usage

- You can check the help by `./magic-pod-api-client help`
- You can check the help of each command by `./magic-pod-api-client help <command name>`

## Examples

### Upload app, run batch test for the app, and wait until the batch run is finished

```
MAGIC_POD_API_TOKEN=<API token displayed on https://magic-pod.com/accounts/api-token/>
MAGIC_POD_ORGANIZATION=<organization>
MAGIC_POD_PROJECT=<project>
FILE_NO=$(./magic-pod-api-client upload-app -a <path to app/ipa/apk>)
./magic-pod-api-client batch-run -s "{\"environment\":\"magic_pod\",\"os\":\"ios\",\"device_type\":\"simulator\",\"version\":\"13.1\",\"model\":\"iPhone 8\",\"app_type\":\"app_file\",\"app_file_number\":${FILE_NO}}"
```

### Run batch test for the app URL and return immediately

```
./magic-pod-api-client batch-run -n -t <API token displayed on https://magic-pod.com/accounts/api-token/> -o <organization> -p <project> -s "{\"environment\":\"magic_pod\",\"os\":\"ios\",\"device_type\":\"simulator\",\"version\":\"13.1\",\"model\":\"iPhone 8\",\"app_type\":\"app_url\",\"app_url\":\"<URL to zipped app/ipa/apk>\"}"
```

## Build from source

Run the following in the top directory of this repository.

```
go get -d .
go build
```

The following is the script to generate 64 bit executables for Mac, Linux, Windows.

```
GOOS=darwin GOARCH=amd64 go build -o ./out/mac64/magic-pod-api-client
GOOS=linux GOARCH=amd64 go build -o ./out/linux64/magic-pod-api-client
GOOS=windows GOARCH=amd64 go build -o ./out/win64/magic-pod-api-client.exe
```
