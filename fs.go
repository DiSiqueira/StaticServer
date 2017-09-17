package main

import (
	"io"
	"os"
	"strconv"
)

type (
	fileServer struct {
		urlList fileTable
	}

	fileTable map[string]File

	file struct {
		contentType ContentType
		path        string
	}

	File interface {
		ContentType() ContentType
		File() (*os.File, error)
	}

	ContentType string
)

const (
	ContentTypeHTML ContentType = "text/html; charset=UTF-8"
)

func NewFile(path string, contentType ContentType) File {
	return &file{
		path:        path,
		contentType: contentType,
	}
}

func (f *file) ContentType() ContentType {
	return f.contentType
}

func (f *file) File() (*os.File, error) {
	return os.Open(f.path)
}

func NewFileTableServer(urlList fileTable) Handler {
	return &fileServer{
		urlList: urlList,
	}
}

func (f *fileServer) ServeHTTP(w ResponseWriter, r *Request) {
	urlFile, ok := f.urlList[r.URL.Path]
	if !ok {
		NotFound(w)
		return
	}

	file, err := urlFile.File()
	if err != nil {
		mytoHTTPError(w, err)
		return
	}
	defer file.Close()

	d, err := file.Stat()
	if err != nil {
		mytoHTTPError(w, err)
		return
	}

	if d.IsDir() {
		NotFound(w)
		return
	}

	w.Header().Set("Content-Type", string(urlFile.ContentType()))
	w.Header().Set("Server", "DStaticServer 0.1")

	sendSize := d.Size()
	w.Header().Set("Content-Length", strconv.FormatInt(sendSize, 10))

	w.WriteHeader(StatusOK)
	io.CopyN(w, file, sendSize)
}

func mytoHTTPError(w ResponseWriter, err error) {
	if os.IsNotExist(err) {
		NotFound(w)
		return
	}
	if os.IsPermission(err) {
		Forbidden(w)
		return
	}
	InternalServerError(w)
	return
}
