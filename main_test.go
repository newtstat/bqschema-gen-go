package main

import (
	"context"
	"os"
	"reflect"
	"testing"

	"cloud.google.com/go/bigquery"
)

const (
	testGoogleApplicationCredentials = "test/serviceaccountnotfound@projectnotfound.iam.gserviceaccount.com.json"
)

func Test_getAllTables_OK(t *testing.T) {
	if os.Getenv(envNameGoogleApplicationCredentials) == "" {
		t.Log("WARN: " + envNameGoogleApplicationCredentials + " is not set")
		return
	}

	var (
		okCtx       = context.Background()
		okProjectID = "bigquery-public-data"
		okClient, _ = bigquery.NewClient(okCtx, okProjectID)
		okDatasetID = "samples"
	)

	if _, err := getAllTables(okCtx, okClient, okDatasetID); err != nil {
		t.Log(err)
		t.Fail()
	}
}

func Test_getAllTables_NG(t *testing.T) {
	var backupValue string

	v, exist := os.LookupEnv(envNameGoogleApplicationCredentials)
	if exist {
		backupValue = v
	}

	_ = os.Setenv(envNameGoogleApplicationCredentials, testGoogleApplicationCredentials)

	var (
		ngCtx       = context.Background()
		ngProjectID = "projectnotfound"
		ngClient, _ = bigquery.NewClient(ngCtx, ngProjectID)
		ngDatasetID = "datasetnotfound"
	)

	if _, err := getAllTables(ngCtx, ngClient, ngDatasetID); err == nil {
		t.Log(err)
		t.Fail()
	}

	if exist {
		_ = os.Setenv(envNameGoogleApplicationCredentials, backupValue)
		return
	}

	_ = os.Unsetenv(envNameGoogleApplicationCredentials)
	return
}

func Test_newGoogleApplicationCredentials(t *testing.T) {
	var (
		noSuchFileOrDirectoryPath = "/no/such/file/or/directory"
		cannotJSONMarshalPath     = "go.mod"
	)

	if _, err := newGoogleApplicationCredentials(noSuchFileOrDirectoryPath); err == nil {
		t.Log(err)
		t.Fail()
	}

	if _, err := newGoogleApplicationCredentials(cannotJSONMarshalPath); err == nil {
		t.Log(err)
		t.Fail()
	}

	if _, err := newGoogleApplicationCredentials(testGoogleApplicationCredentials); err != nil {
		t.Log(err)
		t.Fail()
	}
}

func Test_readFile(t *testing.T) {
	var (
		errNoSuchFileOrDirectoryPath = "/no/such/file/or/directory"
		errIsADirectory              = "."
		probablyExistsPath           = "go.mod"
	)
	if _, err := readFile(errNoSuchFileOrDirectoryPath); err == nil {
		t.Log(err)
		t.Fail()
	}

	if _, err := readFile(errIsADirectory); err == nil {
		t.Log(err)
		t.Fail()
	}

	if _, err := readFile(probablyExistsPath); err != nil {
		t.Log(err)
		t.Fail()
	}
}

func Test_getOptOrEnvOrDefault(t *testing.T) {
	var (
		empty            = ""
		testOptKey       = "testOptKey"
		testOptValue     = "testOptValue"
		testEnvKey       = "TEST_ENV_KEY"
		testEnvValue     = "testEnvValue"
		testDefaultValue = "testDefaultValue"
	)

	{
		v, err := getOptOrEnvOrDefault(empty, empty, empty, empty)
		if err == nil {
			t.Log(err)
			t.Fail()
		}
		if v != empty {
			t.Log(err)
			t.Fail()
		}
	}

	{
		v, err := getOptOrEnvOrDefault(testOptKey, testOptValue, testEnvKey, testDefaultValue)
		if err != nil {
			t.Log(err)
			t.Fail()
		}
		if v != testOptValue {
			t.Log(err)
			t.Fail()
		}
	}

	{
		_ = os.Setenv(testEnvKey, testEnvValue)
		v, err := getOptOrEnvOrDefault(testOptKey, empty, testEnvKey, testDefaultValue)
		if err != nil {
			t.Log(err)
			t.Fail()
		}
		if v != testEnvValue {
			t.Log(err)
			t.Fail()
		}
		_ = os.Unsetenv(testEnvKey)
	}

	{
		v, err := getOptOrEnvOrDefault(testOptKey, empty, testEnvKey, testDefaultValue)
		if err != nil {
			t.Log(err)
			t.Fail()
		}
		if v != testDefaultValue {
			t.Log(err)
			t.Fail()
		}
	}

	{
		v, err := getOptOrEnvOrDefault(testOptKey, empty, testEnvKey, empty)
		if err == nil {
			t.Log(err)
			t.Fail()
		}
		if v != empty {
			t.Log(err)
			t.Fail()
		}
	}
}

func Test_capitalizeInitial(t *testing.T) {
	var (
		empty          = ""
		notCapitalized = "a"
		capitalized    = "A"
	)

	if capitalizeInitial(empty) != empty {
		t.Log()
		t.Fail()
	}

	if capitalizeInitial(notCapitalized) != capitalized {
		t.Log()
		t.Fail()
	}
}

func Test_bigqueryFieldTypeToGoType(t *testing.T) {
	supportedBigqueryFieldTypes := map[bigquery.FieldType]string{
		bigquery.StringFieldType:    reflect.String.String(),
		bigquery.BytesFieldType:     typeOfByteSlice.String(),
		bigquery.IntegerFieldType:   reflect.Int64.String(),
		bigquery.FloatFieldType:     reflect.Float64.String(),
		bigquery.BooleanFieldType:   reflect.Bool.String(),
		bigquery.TimestampFieldType: typeOfGoTime.String(),
		// TODO(djeeno): support bigquery.RecordFieldType
		//bigquery.RecordFieldType: "",
		bigquery.DateFieldType:      typeOfDate.String(),
		bigquery.TimeFieldType:      typeOfTime.String(),
		bigquery.DateTimeFieldType:  typeOfDateTime.String(),
		bigquery.NumericFieldType:   typeOfRat.String(),
		bigquery.GeographyFieldType: reflect.String.String(),
	}

	unsupportedBigqueryFieldTypes := map[bigquery.FieldType]string{
		bigquery.RecordFieldType:               "",
		bigquery.FieldType("unknownFieldType"): "",
	}

	for bigqueryFieldType, typeOf := range supportedBigqueryFieldTypes {
		goType, _, err := bigqueryFieldTypeToGoType(bigqueryFieldType)
		if err != nil {
			t.Log(err)
			t.Fail()
		}
		if goType != typeOf {
			t.Log()
			t.Fail()
		}
	}

	for bigqueryFieldType, typeOf := range unsupportedBigqueryFieldTypes {
		goType, _, err := bigqueryFieldTypeToGoType(bigqueryFieldType)
		if err == nil {
			t.Log(err)
			t.Fail()
		}
		if goType != typeOf {
			t.Log()
			t.Fail()
		}

	}
}
