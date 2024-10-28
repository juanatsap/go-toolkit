package toolkit

import (
	"fmt"
	"image"
	"image/png"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
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
