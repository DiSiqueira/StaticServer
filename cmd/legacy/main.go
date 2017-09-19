package main

import "github.com/disiqueira/StaticServer/pkg/legacy"

func main() {
	urls := legacy.FileTable{}
	urls["/"] = legacy.NewFile("./files/teste.html", legacy.ContentTypeHTML)
	urls["/index"] = legacy.NewFile("./files/index.html", legacy.ContentTypeHTML)

	legacy.ListenAndServe(8080, legacy.NewFileTableServer(urls))
}
