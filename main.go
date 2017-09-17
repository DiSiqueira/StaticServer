package main

func main() {
	urls := fileTable{}
	urls["/"] = NewFile("./files/teste.html", ContentTypeHTML)
	urls["/index"] = NewFile("./files/index.html", ContentTypeHTML)

	ListenAndServe(8080, NewFileTableServer(urls))
}
