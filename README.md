# magic-pod-api-client
Simple and useful wrapper for Magic Pod Web API.

## Install

Download the latest magic-pod-api-client executable from [here](https://github.com/Magic-Pod/magic-pod-api-client/releases) and unzip it to any directory you prefer.

## Usage

- You can check the help by `./magic-pod-api-client help`
- You can check the help of each command by `./magic-pod-api-client help <command name>`

## Examples

### Upload app, run batch test for the app, wait until the batch run is finished, and delete the app if the test passed.

```
export MAGIC_POD_API_TOKEN=<API token displayed on https://magic-pod.com/accounts/api-token/>
export MAGIC_POD_ORGANIZATION=<organization>
export MAGIC_POD_PROJECT=<project>
FILE_NO=$(./magic-pod-api-client upload-app -a <path to app/ipa/apk>)
./magic-pod-api-client batch-run -s "{\"environment\":\"magic_pod\",\"os\":\"ios\",\"device_type\":\"simulator\",\"version\":\"13.1\",\"model\":\"iPhone 8\",\"app_type\":\"app_file\",\"app_file_number\":\"${FILE_NO}\"}"
if [ $? = 0 ];
  ./magic-pod-api-client delete-app -a ${FILE_NO}
fi
```

### Run batch test for the app URL and return immediately

```
./magic-pod-api-client batch-run -n -t <API token displayed on https://magic-pod.com/accounts/api-token/> -o <organization> -p <project> -s "{\"environment\":\"magic_pod\",\"os\":\"ios\",\"device_type\":\"simulator\",\"version\":\"13.1\",\"model\":\"iPhone 8\",\"app_type\":\"app_url\",\"app_url\":\"<URL to zipped app/ipa/apk>\"}"
```

### Run a multi-device pattern for the app URL, and wait until all batch runs are finished

```
./magic-pod-api-client batch-run -n -t <API token displayed on https://magic-pod.com/accounts/api-token/> -o <organization> -p <project> -s "{\"test_settings\":[{\"environment\":\"magic_pod\",\"os\":\"ios\",\"device_type\":\"simulator\",\"version\":\"13.1\",\"model\":\"iPhone 8\",\"app_type\":\"app_url\",\"app_url\":\"<URL to zipped app/ipa/apk>\"},{\"environment\":\"magic_pod\",\"os\":\"ios\",\"device_type\":\"simulator\",\"version\":\"13.1\",\"model\":\"iPhone X\",\"app_type\":\"app_url\",\"app_url\":\"<URL to zipped app/ipa/apk>\"}]\,\"concurrency\": 1}"
```

### Run 2 batch tests for different app URLs in parallel, and wait until all batch runs are finished

```
export MAGIC_POD_API_TOKEN=<API token displayed on https://magic-pod.com/accounts/api-token/>
export MAGIC_POD_ORGANIZATION=<organization>
export MAGIC_POD_PROJECT=<project>

./magic-pod-api-client batch-run -s "{\"environment\":\"magic_pod\",\"os\":\"ios\",\"device_type\":\"simulator\",\"version\":\"13.1\",\"model\":\"iPhone 8\",\"app_type\":\"app_url\",\"app_url\":\"<URL1>\"}" &
PID1=$!

./magic-pod-api-client batch-run -s "{\"environment\":\"magic_pod\",\"os\":\"ios\",\"device_type\":\"simulator\",\"version\":\"13.1\",\"model\":\"iPhone 8\",\"app_type\":\"app_url\",\"app_url\":\"<URL2>\"}" &
PID2=$!

# return values of the wait commands will be magic-pod-api-client's return values.
wait $PID1
wait $PID2
```

## Build from source

Run the following in the top directory of this repository.

```
go get -d .
go build
```

The following is the script to generate 64 bit executables and zip files for Mac, Linux, Windows.

```
# Mac64
GOOS=darwin GOARCH=amd64 go build -o out/mac64/magic-pod-api-client
zip -jq out/mac64_magic-pod-api-client.zip out/mac64/magic-pod-api-client

# Linux64
GOOS=linux GOARCH=amd64 go build -o out/linux64/magic-pod-api-client
zip -jq out/linux64_magic-pod-api-client.zip out/linux64/magic-pod-api-client

# Win64
GOOS=windows GOARCH=amd64 go build -o out/win64/magic-pod-api-client.exe
zip -jq out/win64_magic-pod-api-client.exe.zip out/win64/magic-pod-api-client.exe
```

## Sign and Notarize Mac binary

You need to follow https://g3rv4.com/2019/06/bundling-signing-notarizing-go-application carefully.
The step is like:

1. Build binaries.
2. Create app-specific password for the Apple ID according to [this article](https://support.apple.com/en-us/HT204397).
3. Run the following on the top directory.
4. After a while, you will receive an e-mail that notifies you that the notarization process has finished.

```
# You can check certificate name by `security find-identity -v`
codesign -s <certificate name> -v --timestamp --options runtime out/mac64/magic-pod-api-client
# Zip again
zip -jq out/mac64_magic-pod-api-client.zip out/mac64/magic-pod-api-client
# Basically you need to specify app-specific password
xcrun altool --notarize-app --primary-bundle-id "com.magic-pod.api-client" --username "<Apple ID>" --file out/mac64_magic-pod-api-client.zip
```