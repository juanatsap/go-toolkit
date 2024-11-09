package toolkit

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/log"
)

const randomStringSource = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-+"

// Tools is the type we use to instantiate this module. Any variable of this
// type will have access to all the methods with the receiver *Tools
type Tools struct {
	MaxFileSize        int
	AllowedFileTypes   []string
	MaxJSONSize        int
	AllowUnknownFields bool
}

type JSONResponse struct {
	Error   bool        `json:"error"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// RandomString returns a string of random characters of length n
func (t *Tools) RandomString(n int) string {
	s, r := make([]rune, n), []rune(randomStringSource)
	for i := range s {
		p, _ := rand.Prime(rand.Reader, len(r))
		x, y := p.Uint64(), uint64(len(r))
		s[i] = r[x%y]
	}
	return string(s)
}

// UploadedFile is a struct used to save information about an uploaded file
type UploadedFile struct {
	NewFileName      string
	OriginalFileName string
	FileSize         int64
}

// UploadFiles upload one or more files to a particular location
func (t *Tools) UploadFiles(r *http.Request, uploadDir string, rename ...bool) ([]*UploadedFile, error) {

	renameFile := true
	if len(rename) > 0 {
		renameFile = rename[0]
	}

	uploadedFiles := []*UploadedFile{}

	if t.MaxFileSize == 0 {
		t.MaxFileSize = 1024 * 1024 * 1024 // 1GB
	}

	err := t.CreateDirIfNotExist(uploadDir)
	if err != nil {
		return nil, err
	}

	err = r.ParseMultipartForm(int64(t.MaxFileSize))
	if err != nil {
		log.Error("File greater than 1GB")
		return nil, err
	}

	for _, fHeaders := range r.MultipartForm.File {
		for _, hdr := range fHeaders {
			uploadedFiles, err = func(uploadedFiles []*UploadedFile) ([]*UploadedFile, error) {
				var uploadedFile UploadedFile

				infile, err := hdr.Open()
				if err != nil {
					return nil, err
				}
				defer infile.Close()

				buff := make([]byte, 512)
				_, err = infile.Read(buff)
				if err != nil {
					return nil, err
				}

				// Check suffix
				fileType := http.DetectContentType(buff)
				allowed := false

				if len(t.AllowedFileTypes) > 0 {
					for _, x := range t.AllowedFileTypes {
						if strings.EqualFold(fileType, x) {
							allowed = true
						}
					}
				} else {
					allowed = true
				}

				if !allowed {
					return nil, fmt.Errorf("File type not allowed: %s", fileType)
				}

				_, err = infile.Seek(0, 0)
				if err != nil {
					return nil, err
				}

				if renameFile {
					uploadedFile.NewFileName = fmt.Sprintf("%s%s", t.RandomString(25), filepath.Ext(hdr.Filename))
				} else {
					uploadedFile.NewFileName = hdr.Filename
				}
				// The name
				uploadedFile.OriginalFileName = hdr.Filename
				var outfile *os.File
				defer outfile.Close()

				if outfile, err = os.Create(filepath.Join(uploadDir, uploadedFile.NewFileName)); err != nil {
					return nil, err
				} else {
					fileSize, err := io.Copy(outfile, infile)
					if err != nil {
						return nil, err
					}
					// The size
					uploadedFile.FileSize = fileSize
				}
				uploadedFile.OriginalFileName = hdr.Filename
				uploadedFiles = append(uploadedFiles, &uploadedFile)
				return uploadedFiles, nil
			}(uploadedFiles)

			if err != nil {
				return uploadedFiles, err
			}
		}
	}
	return uploadedFiles, nil
}

// UploadOneFile upload one file to a particular location
func (t *Tools) UploadOneFile(r *http.Request, uploadDir string, rename ...bool) (*UploadedFile, error) {

	renameFile := true
	if len(rename) > 0 {
		renameFile = rename[0]
	}

	uploadedFiles, err := t.UploadFiles(r, uploadDir, renameFile)
	if err != nil {
		return nil, err
	}

	if len(uploadedFiles) > 1 {
		return nil, fmt.Errorf("Only one file can be uploaded at a time")
	}
	return uploadedFiles[0], nil
}

// CreateDirIfNotExist create a directory, and all parent directories if it doesn't exist
func (t *Tools) CreateDirIfNotExist(path string) error {

	const mode = 0755

	if _, err := os.Stat(path); os.IsNotExist(err) {
		err := os.MkdirAll(path, mode)
		if err != nil {
			return err
		}
	}
	return nil
}

// Slugify is a function that returns a slug given a string
func (t *Tools) Slugify(s string) (string, error) {

	if s == "" {
		return "", errors.New("empty string is not permitted")
	}

	re := regexp.MustCompile(`[^a-z0-9]+`)
	slug := strings.Trim(re.ReplaceAllString(strings.ToLower(s), "-"), "-")

	if len(slug) == 0 {
		return "", errors.New("after removing characters, slug is zero length")
	}
	return slug, nil
}

// DownloadStaticFile downloads a file, or sends it to the client. It also force the browser to download the file
// It also allows specification of the file name.
func (t *Tools) DownloadStaticFile(w http.ResponseWriter, r *http.Request, pathName, displayName string) {

	// We want to download the file directly to the client, so we need to set the Content-Disposition header
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", displayName))
	http.ServeFile(w, r, pathName)
}

// ReadJSON tries to read the body of a request and converts it into JSON.
// If there is an error, we write the error in the response and return a 400 status code.
func (t *Tools) ReadJSON(w http.ResponseWriter, r *http.Request, data interface{}) error {

	maxBytes := 1024 * 1024 // 1MB
	if t.MaxFileSize > 0 {
		maxBytes = t.MaxFileSize
	}

	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))

	dec := json.NewDecoder(r.Body)

	if !t.AllowUnknownFields {
		dec.DisallowUnknownFields()
	}

	err := dec.Decode(data)

	if err != nil {

		var syntaxError *json.SyntaxError
		var unmarshalTypeError *json.UnmarshalTypeError
		var invalidUnmarshalError *json.InvalidUnmarshalError

		switch {

		case errors.As(err, &syntaxError):
			return fmt.Errorf("body contains badly-formed JSON (at character %d)", syntaxError.Offset)

		case errors.Is(err, io.ErrUnexpectedEOF):
			return errors.New("body contains badly-formed JSON")

		case errors.As(err, &unmarshalTypeError):
			if unmarshalTypeError.Field != "" {
				return fmt.Errorf("body contains incorrect JSON type for field %q", unmarshalTypeError.Field)
			}

		case errors.As(err, &invalidUnmarshalError):
			return fmt.Errorf("body contains incorrect JSON type (at character %d)", invalidUnmarshalError.Type.Bits())

		case errors.Is(err, io.EOF):
			return errors.New("body must not be empty")

		case strings.HasPrefix(err.Error(), "json: unknown field "):
			fieldName := strings.TrimPrefix(err.Error(), "json: unknown field ")
			return fmt.Errorf("body contains unknown key %s", fieldName)

		case err.Error() == "http: request body too large":
			return fmt.Errorf("body must not be larger than %d bytes", maxBytes)

		case errors.As(err, &invalidUnmarshalError):
			return fmt.Errorf("error unmarshalling JSON: %s", err.Error())

		default:
			return err

		}
	}
	// It will try to decode more JSON from that fail
	err = dec.Decode(&struct{}{})
	if err != io.EOF {
		return errors.New("body must only contain a single JSON value")
	}
	return nil
}

// WriteJSON tries to write the response as JSON.
func (t *Tools) WriteJSON(w http.ResponseWriter, status int, data interface{}, headers ...http.Header) error {
	out, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if len(headers) > 0 {
		for key, value := range headers[0] {
			w.Header()[key] = value
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(out)
	if err != nil {
		return err
	}
	return nil
}

// Error JSON takes an error and a status code, and writes a JOSN error message to the client
func (t *Tools) ErrorJSON(w http.ResponseWriter, err error, status ...int) error {

	statusCode := http.StatusBadRequest
	if len(status) > 0 {
		statusCode = status[0]
	}
	payload := JSONResponse{
		Error:   true,
		Message: err.Error(),
	}
	return t.WriteJSON(w, statusCode, payload)
}

// PushJSONToRemote pushes arbitrary JSON data to a remote endpoint and returns the response, status code, and error if any
// The final parameter is an optional http client. If none is specified, we use the standard http.Client
func (t *Tools) PushJSONToRemote(uri string, data interface{}, client ...*http.Client) (*http.Response, int, error) {
	// create json
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, 0, err
	}

	// checks for custom http client
	httpClient := &http.Client{}

	if len(client) > 0 {
		httpClient = client[0]
	}

	// create request
	req, err := http.NewRequest("POST", uri, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	// send request
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}

	// !!!
	defer resp.Body.Close()

	// send response back
	return resp, resp.StatusCode, nil
}
