package toolkit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

var uploadTests = []struct {
	name          string
	allowedTypes  []string
	rename        bool
	errorExpected bool
}{
	{name: "allowed no rename", allowedTypes: []string{"image/jpeg", "image/png"}, rename: false, errorExpected: false},
	{name: "allowed rename", allowedTypes: []string{"image/jpeg", "image/png"}, rename: true, errorExpected: false},
	{name: "not allowed no rename", allowedTypes: []string{"image/jpeg"}, rename: false, errorExpected: true},
	{name: "not allowed rename", allowedTypes: []string{"image/jpeg"}, rename: true, errorExpected: true},
}

var slugsTests = []struct {
	name          string
	s             string
	expected      string
	errorExpected bool
}{
	{name: "empty string", s: "", expected: "", errorExpected: true},
	{name: "underscore", s: "_", expected: "", errorExpected: true},
	{name: "two words", s: "abc def", expected: "abc-def", errorExpected: false},
	{name: "two words with spaces", s: "abc def ghi", expected: "abc-def-ghi", errorExpected: false},
}

var jsonTests = []struct {
	name          string
	json          string
	error         bool
	errorMessage  string
	maxSize       int
	allowUnknown  bool
	errorExpected bool
}{
	{name: "good JSON", json: `{"foo": "bar"}`, errorExpected: false, maxSize: 1024, allowUnknown: false},
	{name: "badly formated JSON", json: `{foo": "bar"}`, errorExpected: true, maxSize: 1024, allowUnknown: false},
	{name: "incorrect type", json: `{"foo": 3.14159}`, errorExpected: true, maxSize: 1024, allowUnknown: false},
	{name: "two json files", json: `{"foo": "1"}{"alpha": "2"}`, errorExpected: true, maxSize: 1024, allowUnknown: false},
	{name: "empty body", json: ``, errorExpected: true, maxSize: 1024, allowUnknown: false},
	{name: "missing closing brace", json: `{"foo": "bar`, errorExpected: true, maxSize: 1024, allowUnknown: false},
	{name: "missing colon", json: `{"foo" "bar"}`, errorExpected: true, maxSize: 1024, allowUnknown: false},
	{name: "missing comma and colon", json: `{"foo" "bar" "alpha" "beta"}`, errorExpected: true, maxSize: 1024, allowUnknown: false},
	{name: "missing comma", json: `{"foo": "bar" "alpha": "beta"}`, errorExpected: true, maxSize: 1024, allowUnknown: false},
	{name: "extra comma", json: `{"foo": "bar",`, errorExpected: true, maxSize: 1024, allowUnknown: false},
	{name: "not json", json: `Hello, World!`, errorExpected: true, maxSize: 1024, allowUnknown: false},
	{name: "unknown field in JSON", json: `{"foo": "bar", "alpha": "beta"}`, errorExpected: true, maxSize: 1024, allowUnknown: false},
	{name: "allow unknown fields", json: `{"foo": "bar", "alpha": "beta"}`, errorExpected: false, maxSize: 1024, allowUnknown: true},
	{name: "max size", json: `{"foo": "bar", "alpha": "beta"}`, errorExpected: true, maxSize: 5, allowUnknown: false},
	{name: "missing field name", json: `{:"bar", "alpha": "beta"}`, errorExpected: true, maxSize: 1024, allowUnknown: false},
	{name: "file size too large", json: `{"foo": "bar"}`, errorExpected: true, maxSize: 5, allowUnknown: false},
	{name: "not json", json: `Hello, World!`, errorExpected: true, maxSize: 1024, allowUnknown: false},
}

type RoundTripFunc func(req *http.Request) *http.Response

func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}
func NewTestClient(fn RoundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func TestTools_RandomString(t *testing.T) {
	var tools Tools
	const length = 10
	randomString := tools.RandomString(length)
	if len(randomString) != length {
		t.Errorf("Expected %d, got %d", length, len(randomString))
	}
}
func TestTools_UploadFiles(t *testing.T) {

	for _, e := range uploadTests {
		// set up a pipe to avoid buffering
		pr, pw := io.Pipe()
		writer := multipart.NewWriter(pw)

		// I want to capture all the output from this function
		wg := sync.WaitGroup{}

		//
		wg.Add(1)

		go func() {
			defer writer.Close()
			defer wg.Done()

			// create the form data headers
			part, err := writer.CreateFormFile("file", "./testdata/legion-xiii-logo.png")
			if err != nil {
				t.Error(err)
			}

			f, err := os.Open("./testdata/legion-xiii-logo.png")
			if err != nil {
				t.Error(err)
			}
			defer f.Close()

			img, _, err := image.Decode(f)
			if err != nil {
				t.Error("Error decoding image", err)
			}

			err = png.Encode(part, img)
			if err != nil {
				t.Error(err)
			}

			// add the other fields
		}()

		// read from the pipe which receives data
		request := httptest.NewRequest("POST", "/", pr)
		request.Header.Add("Content-Type", writer.FormDataContentType())

		var testTools Tools
		testTools.AllowedFileTypes = e.allowedTypes
		uploadedFiles, err := testTools.UploadFiles(request, "./testdata/uploads/", e.rename)
		if err != nil && !e.errorExpected {
			t.Error(err)
		}
		if !e.errorExpected {
			if _, err := os.Stat(fmt.Sprintf("./testdata/uploads/%s", uploadedFiles[0].NewFileName)); os.IsNotExist(err) {
				t.Errorf("Expected file %s to exist: %s", uploadedFiles[0].NewFileName, err.Error())
			}

			// clean up
			_ = os.Remove(fmt.Sprintf("./testdata/uploads/%s", uploadedFiles[0].NewFileName))
		}

		if !e.errorExpected && err != nil {
			t.Errorf("%s: error expected but got none", e.name)
		}

		if e.errorExpected && err == nil {
			t.Errorf("%s: error expected but got none", e.name)
		}

		wg.Wait()
	}
}
func TestTools_UploadOneFile(t *testing.T) {

	e := uploadTests[0]
	// set up a pipe to avoid buffering
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer writer.Close()

		// create the form data headers
		part, err := writer.CreateFormFile("file", "./testdata/legion-xiii-logo.png")
		if err != nil {
			t.Error(err)
		}

		f, err := os.Open("./testdata/legion-xiii-logo.png")
		if err != nil {
			t.Error(err)
		}
		defer f.Close()

		img, _, err := image.Decode(f)
		if err != nil {
			t.Error("Error decoding image", err)
		}

		err = png.Encode(part, img)
		if err != nil {
			t.Error(err)
		}
	}()

	// read from the pipe which receives data
	request := httptest.NewRequest("POST", "/", pr)
	request.Header.Add("Content-Type", writer.FormDataContentType())

	var testTools Tools

	uploadedFiles, err := testTools.UploadOneFile(request, "./testdata/uploads/", false)
	if err != nil {
		t.Error(err)
	}

	if _, err := os.Stat(fmt.Sprintf("./testdata/uploads/%s", uploadedFiles.OriginalFileName)); os.IsNotExist(err) {
		t.Errorf("Expected file to exist: %s", err.Error())
	}

	// clean up
	_ = os.Remove(fmt.Sprintf("./testdata/uploads/%s", uploadedFiles.OriginalFileName))

	if !e.errorExpected && err != nil {
		t.Errorf("%s: error expected but got none", err)
	}

	if e.errorExpected && err == nil {
		t.Errorf("%s: no error expected but got one", err)
	}
}
func TestTools_CreateDirIfNotExist(t *testing.T) {
	var testTools Tools
	err := testTools.CreateDirIfNotExist("./testdata/uploads/")
	if err != nil {
		t.Error(err)
	}

	err = testTools.CreateDirIfNotExist("./testdata/uploads/")
	if err != nil {
		t.Error(err)
	}

	_ = os.Remove("./testdata/uploads")
}
func TestTools_Slugify(t *testing.T) {
	var testTools Tools
	for _, e := range slugsTests {
		slug, err := testTools.Slugify(e.s)

		if err != nil && !e.errorExpected {
			t.Errorf("%s failed: %s", e.name, err)
		}

		if !e.errorExpected && slug != e.expected {
			t.Errorf("%s: expected %s, got %s", e.name, e.expected, slug)
		}

		if err == nil && e.errorExpected {
			t.Errorf("%s didn't return an error", e.name)
		}
	}
}
func TestTools_DownloadStaticFile(t *testing.T) {
	rr := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)

	var testTools Tools

	testTools.DownloadStaticFile(rr, req, "./testdata/", "legion-xiii-logo.png", "legion-xiii-logo-download.png")

	res := rr.Result()

	defer res.Body.Close()

	if res.Header["Content-Length"][0] != "148640" {
		t.Errorf("Wrong content length, got %s", res.Header["Content-Length"][0])
	}

	if res.Header["Content-Disposition"][0] != "attachment; filename=\"legion-xiii-logo-download.png\"" {
		t.Errorf("Wrong content disposition, got %s", res.Header["Content-Disposition"][0])
	}

	// Check for an error when I try to read from the response body
	_, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
}

func TestTools_ReadJSON(t *testing.T) {
	var testTools Tools

	for _, e := range jsonTests {
		// set the max file size
		testTools.MaxFileSize = e.maxSize

		// allow / disallow unknown fields
		testTools.AllowUnknownFields = e.allowUnknown

		// declare a variable to read the decoded JSON into
		var decodedJSON struct {
			Foo string `json:"foo"`
		}

		// create a request with the body
		req, err := http.NewRequest("POST", "/", bytes.NewReader([]byte(e.json)))
		if err != nil {
			t.Error(err)
		}

		// create a recorder
		rr := httptest.NewRecorder()

		err = testTools.ReadJSON(rr, req, &decodedJSON)

		if e.errorExpected && err == nil {
			t.Errorf("%s: error expected but got none", e.name)
		}

		if !e.errorExpected && err != nil {
			t.Errorf("%s: error not expected but got one", e.name)
		}

		if e.errorExpected && err != nil {
			if !strings.Contains(err.Error(), e.errorMessage) {
				t.Errorf("%s: error message not expected, got %s", e.name, err.Error())
			}
		}

		if !e.errorExpected && err == nil {
			// if there is no error, check the decoded data
			if decodedJSON.Foo != "bar" {
				t.Errorf("%s: expected foo to be bar, got %s", e.name, decodedJSON.Foo)
			}
		}

		// check for an error when I try to read from the response body
		_, err = ioutil.ReadAll(rr.Result().Body)
		if err != nil {
			t.Error(err)
		}

		// reset the max file size
		testTools.MaxFileSize = 0

		// reset the allowUnknownFields
		testTools.AllowUnknownFields = false
	}
}

func TestTools_WriteJSON(t *testing.T) {
	var testTools Tools

	rr := httptest.NewRecorder()
	payload := JSONResponse{
		Error:   false,
		Message: "foo",
	}

	headers := make(http.Header)
	headers.Add("FOO", "BAR")

	err := testTools.WriteJSON(rr, http.StatusOK, payload, headers)
	if err != nil {
		t.Errorf("failed to write JSON: %v", err)
	}
}

func TestTools_ErrorJSON(t *testing.T) {
	var testTools Tools

	rr := httptest.NewRecorder()
	err := testTools.ErrorJSON(rr, errors.New("some error"), http.StatusUnprocessableEntity)
	if err != nil {
		t.Errorf("failed to write JSON: %v", err)
	}

	var payload JSONResponse
	decoder := json.NewDecoder(rr.Body)
	err = decoder.Decode(&payload)

	if err != nil {
		t.Errorf("failed to decode JSON: %v", err)
	}

	if !payload.Error {
		t.Errorf("expected error, got no error")
	}

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected status code %d, got %d", http.StatusUnprocessableEntity, rr.Code)
	}
}

func TestTools_PushJSONToRemote(t *testing.T) {
	client := NewTestClient(func(req *http.Request) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBuffer([]byte(`{"foo": "bar"}`))),
			Header:     make(http.Header),
		}
	})

	var testTools Tools
	var foo = struct {
		Bar string `json:"bar"`
	}{
		Bar: "bar",
	}

	_, _, err := testTools.PushJSONToRemote("http://test.com", foo, client)

	if err != nil {
		t.Errorf("failed to push JSON: %v", err)
	}
}
