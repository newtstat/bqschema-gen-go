//go:generate go run github.com/djeeno/bqtableschema

package main

import (
	"context"
	"flag"
	"fmt"
	"go/format"
	"io/ioutil"
	"log"
	"math/big"
	"os"
	"reflect"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/civil"
	"golang.org/x/tools/imports"
	"google.golang.org/api/iterator"
)

const (
	// optName
	optNameProjectID  = "project"
	optNameDataset    = "dataset"
	optNameKeyFile    = "keyfile"
	optNameOutputFile = "output"
	// envName
	envNameGoogleApplicationCredentials = "GOOGLE_APPLICATION_CREDENTIALS"
	envNameGCloudProjectID              = "GCLOUD_PROJECT_ID"
	envNameBigQueryDataset              = "BIGQUERY_DATASET"
	envNameOutputFile                   = "OUTPUT_FILE"
	// defaultValue
	defaultValueEmpty      = ""
	defaultValueOutputFile = "bqtableschema.generated.go"
)

var (
	// optValue
	optValueProjectID  string
	optValueDataset    string
	optValueKeyFile    string
	optValueOutputPath string
)

func main() {
	flag.StringVar(&optValueProjectID, optNameProjectID, defaultValueEmpty, "")
	flag.StringVar(&optValueDataset, optNameDataset, defaultValueEmpty, "")
	flag.StringVar(&optValueKeyFile, optNameKeyFile, defaultValueEmpty, "path to service account json key file")
	flag.StringVar(&optValueOutputPath, optNameOutputFile, defaultValueEmpty, "path to output the generated code")
	flag.Parse()

	ctx := context.Background()

	if err := Run(ctx); err != nil {
		log.Fatalf("Run: %v\n", err)
	}
}

// Run is effectively a `main` function.
// It is separated from the `main` function because of addressing an issue where` defer` is not executed when `os.Exit` is executed.
func Run(ctx context.Context) error {

	filePath, err := getOptOrEnvOrDefault(optNameOutputFile, optValueOutputPath, envNameOutputFile, defaultValueOutputFile)
	if err != nil {
		return fmt.Errorf("getOptOrEnvOrDefault: %w", err)
	}

	keyfile, err := getOptOrEnvOrDefault(optNameKeyFile, optValueKeyFile, envNameGoogleApplicationCredentials, "")
	if err != nil {
		return fmt.Errorf("getOptOrEnvOrDefault: %w", err)
	}

	project, err := getOptOrEnvOrDefault(optNameProjectID, optValueProjectID, envNameGCloudProjectID, "")
	if err != nil {
		return fmt.Errorf("getOptOrEnvOrDefault: %w", err)
	}

	dataset, err := getOptOrEnvOrDefault(optNameDataset, optValueDataset, envNameBigQueryDataset, "")
	if err != nil {
		return fmt.Errorf("getOptOrEnvOrDefault: %w", err)
	}

	// set GOOGLE_APPLICATION_CREDENTIALS for Google Cloud SDK
	if os.Getenv(envNameGoogleApplicationCredentials) != keyfile {
		if err := os.Setenv(envNameGoogleApplicationCredentials, keyfile); err != nil {
			return fmt.Errorf("os.Setenv: %w", err)
		}
	}

	client, err := bigquery.NewClient(ctx, project)
	if err != nil {
		return fmt.Errorf("bigquery.NewClient: %w", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("client.Close: %v\n", err)
		}
	}()

	generatedCode, err := Generate(ctx, client, dataset)
	if err != nil {
		return fmt.Errorf("Generate: %w", err)
	}

	// NOTE(djeeno): output
	if err := ioutil.WriteFile(filePath, generatedCode, 0644); err != nil {
		return fmt.Errorf("ioutil.WriteFile: %w", err)
	}

	return nil
}

func Generate(ctx context.Context, client *bigquery.Client, dataset string) (generatedCode []byte, err error) {

	const head = `// Code generated by go run github.com/djeeno/bqtableschema; DO NOT EDIT.

//go:generate go run github.com/djeeno/bqtableschema

package bqtableschema

`

	tables, err := getAllTables(ctx, client, dataset)
	if err != nil {
		return nil, fmt.Errorf("getAllTables: %w", err)
	}

	var tail string
	var importPackages []string
	for _, table := range tables {
		structCode, pkgs, err := generateTableSchemaCode(ctx, table)
		if err != nil {
			log.Printf("generateTableSchemaCode: %v\n", err)
			continue
		}

		if len(pkgs) > 0 {
			importPackages = append(importPackages, pkgs...)
		}
		tail = tail + structCode
	}

	importCode := generateImportPackagesCode(importPackages)

	// NOTE(djeeno): combine
	code := head + importCode + tail

	gen := []byte(code)

	genFmt, err := format.Source(gen)
	if err != nil {
		return nil, fmt.Errorf("format.Source: %w", err)
	}

	genImports, err := imports.Process("", genFmt, nil)
	if err != nil {
		return nil, fmt.Errorf("imports.Process: %w", err)
	}

	return genImports, nil
}

func generateImportPackagesCode(importPackages []string) (generatedCode string) {
	importPackagesUniq := make(map[string]bool)

	for _, pkg := range importPackages {
		importPackagesUniq[pkg] = true
	}

	switch {
	case len(importPackagesUniq) == 0:
		generatedCode = ""
	case len(importPackagesUniq) == 1:
		for pkg := range importPackagesUniq {
			generatedCode = "import \"" + pkg + "\"\n"
		}
		generatedCode = generatedCode + "\n"
	case len(importPackagesUniq) >= 2:
		generatedCode = "import (\n"
		for pkg := range importPackagesUniq {
			generatedCode = generatedCode + "\t\"" + pkg + "\"\n"
		}
		generatedCode = generatedCode + ")\n\n"
	}

	return generatedCode
}

func generateTableSchemaCode(ctx context.Context, table *bigquery.Table) (generatedCode string, importPackages []string, err error) {
	if len(table.TableID) == 0 {
		return "", nil, fmt.Errorf("*bigquery.Table.TableID is empty. *bigquery.Table struct dump: %#v", table)
	}
	structName := capitalizeInitial(table.TableID)

	md, err := table.Metadata(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("table.Metadata: %w", err)
	}

	// NOTE(djeeno): structs
	generatedCode = "// " + structName + " is BigQuery Table `" + md.FullID + "` schema struct.\n" +
		"// Description: " + md.Description + "\n" +
		"type " + structName + " struct {\n"

	schemas := []*bigquery.FieldSchema(md.Schema)

	for _, schema := range schemas {
		goTypeStr, pkg, err := bigqueryFieldTypeToGoType(schema.Type)
		if err != nil {
			return "", nil, fmt.Errorf("bigqueryFieldTypeToGoType: %w", err)
		}
		if pkg != "" {
			importPackages = append(importPackages, pkg)
		}
		generatedCode = generatedCode + "\t" + capitalizeInitial(schema.Name) + " " + goTypeStr + " `bigquery:\"" + schema.Name + "\"`\n"
	}
	generatedCode = generatedCode + "}\n"

	return generatedCode, importPackages, nil
}

func getAllTables(ctx context.Context, client *bigquery.Client, datasetID string) (tables []*bigquery.Table, err error) {
	tableIterator := client.Dataset(datasetID).Tables(ctx)
	for {
		table, err := tableIterator.Next()
		if err != nil {
			if err == iterator.Done {
				break
			}
			return nil, fmt.Errorf("tableIterator.Next: %w", err)
		}
		tables = append(tables, table)
	}
	return tables, nil
}

func readFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("os.Open: %w", err)
	}

	bytea, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("ioutil.ReadAll: %w", err)
	}

	return bytea, nil
}

func getOptOrEnvOrDefault(optKey, optValue, envKey, defaultValue string) (string, error) {
	if optKey == "" {
		return "", fmt.Errorf("optKey is empty")
	}

	if optValue != "" {
		return optValue, nil
	}

	envValue := os.Getenv(envKey)
	if envValue != "" {
		return envValue, nil
	}

	if defaultValue != "" {
		return defaultValue, nil
	}

	return "", fmt.Errorf("set option -%s, or set environment variable %s", optKey, envKey)
}

func capitalizeInitial(s string) (capitalized string) {
	if len(s) == 0 {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// NOTE(djeeno): ref. https://github.com/googleapis/google-cloud-go/blob/f37f118c87d4d0a77a554515a430ae06e5852294/bigquery/schema.go#L216
var typeOfByteSlice = reflect.TypeOf([]byte{})

// NOTE(djeeno): ref. https://github.com/googleapis/google-cloud-go/blob/f37f118c87d4d0a77a554515a430ae06e5852294/bigquery/params.go#L81-L87
var (
	typeOfDate     = reflect.TypeOf(civil.Date{})
	typeOfTime     = reflect.TypeOf(civil.Time{})
	typeOfDateTime = reflect.TypeOf(civil.DateTime{})
	typeOfGoTime   = reflect.TypeOf(time.Time{})
	typeOfRat      = reflect.TypeOf(&big.Rat{})
)

func bigqueryFieldTypeToGoType(bigqueryFieldType bigquery.FieldType) (goType string, pkg string, err error) {
	switch bigqueryFieldType {
	// NOTE(djeeno): ref. https://github.com/googleapis/google-cloud-go/blob/f37f118c87d4d0a77a554515a430ae06e5852294/bigquery/schema.go#L342-L343
	case bigquery.BytesFieldType:
		return typeOfByteSlice.String(), "", nil

	// NOTE(djeeno): ref. https://github.com/googleapis/google-cloud-go/blob/f37f118c87d4d0a77a554515a430ae06e5852294/bigquery/schema.go#L344-L358
	case bigquery.DateFieldType:
		return typeOfDate.String(), typeOfDate.PkgPath(), nil
	case bigquery.TimeFieldType:
		return typeOfTime.String(), typeOfTime.PkgPath(), nil
	case bigquery.DateTimeFieldType:
		return typeOfDateTime.String(), typeOfDateTime.PkgPath(), nil
	case bigquery.TimestampFieldType:
		return typeOfGoTime.String(), typeOfGoTime.PkgPath(), nil
	case bigquery.NumericFieldType:
		// NOTE(djeeno): The *T (pointer type) does not return the package path.
		//               ref. https://github.com/golang/go/blob/f0ff6d4a67ec9a956aa655d487543da034cf576b/src/reflect/type.go#L83
		return typeOfRat.String(), reflect.TypeOf(big.Rat{}).PkgPath(), nil

	// NOTE(djeeno): ref. https://github.com/googleapis/google-cloud-go/blob/f37f118c87d4d0a77a554515a430ae06e5852294/bigquery/schema.go#L362-L364
	case bigquery.IntegerFieldType:
		return reflect.Int64.String(), "", nil

	// NOTE(djeeno): ref. https://github.com/googleapis/google-cloud-go/blob/f37f118c87d4d0a77a554515a430ae06e5852294/bigquery/schema.go#L368-L371
	case bigquery.RecordFieldType:
		// TODO(djeeno): support bigquery.RecordFieldType
		return "", "", fmt.Errorf("bigquery.FieldType not supported. bigquery.FieldType=%s", bigqueryFieldType)

	// NOTE(djeeno): ref. https://github.com/googleapis/google-cloud-go/blob/f37f118c87d4d0a77a554515a430ae06e5852294/bigquery/schema.go#L394-L399
	case bigquery.StringFieldType, bigquery.GeographyFieldType:
		return reflect.String.String(), "", nil
	case bigquery.BooleanFieldType:
		return reflect.Bool.String(), "", nil
	case bigquery.FloatFieldType:
		return reflect.Float64.String(), "", nil

	// NOTE(djeeno): ref. https://github.com/googleapis/google-cloud-go/blob/f37f118c87d4d0a77a554515a430ae06e5852294/bigquery/schema.go#L400-L401
	default:
		return "", "", fmt.Errorf("bigquery.FieldType not supported. bigquery.FieldType=%s", bigqueryFieldType)
	}
}
