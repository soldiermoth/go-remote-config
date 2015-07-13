package remoteconfig

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
)

const (
	DEFAULT_S3_EXPIRY   uint   = 60
	DEFAULT_S3_ENDPOINT string = ""
)

type Validater interface {
	Validate() error
}

type TagOptions struct {
	Optional bool
}

// Downloads a configuration JSON file from S3.
// Parses it to a particular struct type and runs a validation.
// URL should be of the format s3://bucket/path/file.json
func LoadConfigFromS3(configURL string, configRegion AWSRegion, configEndpoint string, configStruct interface{}) error {
	// Build a Signed URL to the config file in S3
	signedURL, err := BuildSignedS3URL(configURL, configRegion, DEFAULT_S3_EXPIRY, configEndpoint)
	if err != nil {
		return err
	}

	return DownloadJSONValidate(signedURL, configStruct)
}

// Downloads JSON from a URL, decodes it and then validates.
func DownloadJSONValidate(signedURL string, configStruct interface{}) error {
	// Download the config file from S3
	resp, err := http.Get(signedURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check that we got a valid response code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Download of JSON failed, URL = %s, Response Code = %d", signedURL, resp.StatusCode)
	}

	// Do a streaming JSON decode
	dec := json.NewDecoder(resp.Body)
	if err = dec.Decode(configStruct); err != nil {
		return fmt.Errorf("Failed to decode JSON, with error, %s", err.Error())
	}

	// Run validation on the config
	err = validateConfigWithReflection(configStruct)
	if err != nil {
		return err
	}

	return nil
}

func readTags(typeField reflect.StructField) *TagOptions {
	tags := typeField.Tag.Get("remoteconfig")

	return &TagOptions{
		Optional: strings.Contains(tags, "optional"),
	}
}

func handlePtr(typeOf reflect.Type, valueOf reflect.Value) error {
	//fmt.Printf("Got ptr. valueOf = %v, typeOf = %v, typeOf.Name() = %s\n", valueOf, typeOf, typeOf.Elem().Name())

	// Gets a refection Type value for the Validater interface
	validaterType := reflect.TypeOf((*Validater)(nil)).Elem()
	// If the Validater interface is implemented, call the Validate method
	if typeOf.Implements(validaterType) {
		if err := valueOf.Interface().(Validater).Validate(); err != nil {
			return fmt.Errorf("Validater Field: %s, failed to validate with error, %s", typeOf.Elem().Name(), err)
		}
		// If there is a Validate method, this is the only validation that should be done for this Ptr
		return nil
	}

	if err := validateConfigWithReflection(valueOf.Elem().Interface()); err != nil {
		return fmt.Errorf("Sub Field of %s, failed to validate with error, %s", typeOf.Elem().Name(), err)
	}

	return nil
}

func handleStruct(typeOf reflect.Type, valueOf reflect.Value) error {
	////fmt.Printf("Got struct. valueOf = %v, typeOf = %v\n", valueOf, typeOf)

	for i := 0; i < valueOf.NumField(); i++ {
		////fmt.Printf("Struct Field. valueOf.Field(%d) = %v, typeOf.Field(%d) = %v, typeOf.Field(%d).Type.Kind() = %v\n", i, valueOf.Field(i), i, typeOf.Field(i), i, typeOf.Field(i).Type.Kind())

		tags := readTags(typeOf.Field(i))

		// Handle an optional field
		if valueOf.Field(i).IsNil() && !tags.Optional {
			return fmt.Errorf("Field: %s, not set", typeOf.Field(i).Name)
		} else if valueOf.Field(i).IsNil() && tags.Optional {
			continue
		}

		if valueOf.Field(i).Interface() != nil && !valueOf.Field(i).IsNil() {
			if err := validateConfigWithReflection(valueOf.Field(i).Interface()); err != nil {
				return err
			}
		}
	}
	return nil
}

func handleArraySlice(typeOf reflect.Type, valueOf reflect.Value) error {
	fmt.Printf("Got slice or array. valueOf = %v, typeOf = %v, valueOf.Type().Name() = %s, typeOf.Name() = %s\n", valueOf, typeOf, valueOf.Type().Name(), typeOf.Name())
	if valueOf.Len() <= 0 {
		return fmt.Errorf("Slice/Array Field: %s, is empty", valueOf.Type().Name())
	}
	for i := 0; i < valueOf.Len(); i++ {
		if err := validateConfigWithReflection(valueOf.Index(i).Interface()); err != nil {
			return err
		}
	}
	return nil
}

func handleMap(typeOf reflect.Type, valueOf reflect.Value) error {
	//fmt.Printf("Got map. valueOf = %v, typeOf = %v\n", valueOf, typeOf)
	if valueOf.Len() <= 0 {
		return fmt.Errorf("Map Field: %s, is empty", valueOf.Type().Name())
	}
	for _, k := range valueOf.MapKeys() {
		if err := validateConfigWithReflection(valueOf.MapIndex(k).Interface()); err != nil {
			return err
		}
	}
	return nil
}

func handleString(typeOf reflect.Type, valueOf reflect.Value) error {
	//fmt.Printf("Got string. valueOf = %v, typeOf = %v Value = %s\n", valueOf, typeOf, valueOf.Interface().(string))
	if valueOf.Interface().(string) == "" {
		return fmt.Errorf("String Field: %s, contains an empty string", valueOf.Type().Name())
	}
	return nil
}

// Validates a configuration struct.
// Uses reflection to determine and call the correct Validation methods for each type.
func validateConfigWithReflection(c interface{}) error {
	valueOf := reflect.ValueOf(c)
	typeOf := reflect.TypeOf(c)

	switch typeOf.Kind() {
	case reflect.Ptr:
		return handlePtr(typeOf, valueOf)
	case reflect.Struct:
		return handleStruct(typeOf, valueOf)
	case reflect.Slice, reflect.Array:
		return handleArraySlice(typeOf, valueOf)
	case reflect.Map:
		return handleMap(typeOf, valueOf)
	case reflect.String:
		return handleString(typeOf, valueOf)
	}

	return nil
}

/*
// Validates a configuration struct.
// Uses reflection to determine and call the correct Validation methods for each type.
func validateConfigWithReflection(c interface{}) error {
	valueElem := reflect.ValueOf(c).Elem()
	typeElem := reflect.TypeOf(c).Elem()

	// Gets a refection Type value for the Validater interface
	validaterType := reflect.TypeOf((*Validater)(nil)).Elem()

	// If the Validater interface is implemented, call the Validate method
	if typeElem.Implements(validaterType) {
		if err := valueElem.Interface().(Validater).Validate(); err != nil {
			return fmt.Errorf("Validater Field: %s, failed to validate with error, %s", typeElem.Name(), err)
		}
	}

	for i := 0; i < valueElem.NumField(); i++ {
		valueField := valueElem.Field(i)
		typeField := typeElem.Field(i)


		tags := readTags(typeField)

		// Handle an optional field
		if valueField.IsNil() && !tags.Optional {
			return fmt.Errorf("Field: %s, not set", typeField.Name)
		} else if valueField.IsNil() && tags.Optional {
			continue
		}

		// Handle a slice type
		if valueField.Kind() == reflect.Slice {
			if valueField.Len() <= 0 {
				return fmt.Errorf("Slice Field: %s, is empty", typeField.Name)
			}
			for i := 0; i < valueField.Len(); i++ {
				if err := validateConfigWithReflection(valueField.Index(i).Interface()); err != nil {
					return err
				}
			}
			continue
		}

		// If this is a string field, check that it isn't empty (unless optional)
		if s, ok := valueField.Interface().(*string); ok {
			if *s == "" {
				return fmt.Errorf("String Field: %s, contains an empty string", typeField.Name)
			}
			continue
		}

		// If the Validater interface is implemented, call the Validate method
		if typeField.Type.Implements(validaterType) {
			if err := valueField.Interface().(Validater).Validate(); err != nil {
				return fmt.Errorf("Validater Field: %s, failed to validate with error, %s", typeField.Name, err)
			}
			continue
		}

		// If this field is a struct type, validate it with reflection
		// We can/should only check the sub-fields of a Struct
		if valueField.Elem().Kind() == reflect.Struct && valueField.Elem().NumField() > 0 {
			if err := validateConfigWithReflection(valueField.Interface()); err != nil {
				return fmt.Errorf("Sub Field of %s, failed to validate with error, %s", typeField.Name, err)
			}
		}
	}

	return nil
}*/
