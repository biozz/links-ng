package web

import (
	"embed"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"os"

	"github.com/labstack/echo/v5"
)

//go:embed static/*
var StaticFS embed.FS

//go:embed templates/*
var TemplatesFS embed.FS

type Templates struct {
	dev       bool
	fsys      fs.FS
	templates map[string]*template.Template
}

func NewTemplates(dev bool) *Templates {
	fsys, _ := fs.Sub(TemplatesFS, "templates")
	if dev {
		fsys = os.DirFS("web/templates")
	}
	t := Templates{dev: dev, fsys: fsys}
	t.Must()
	return &t
}

func (t *Templates) Must() {
	t.templates = map[string]*template.Template{
		"index":      template.Must(template.New("").ParseFS(t.fsys, "index.html.tmpl", "layout.html.tmpl")),
		"items":      template.Must(template.New("").ParseFS(t.fsys, "items.html.tmpl")),
		"logs":       template.Must(template.New("").ParseFS(t.fsys, "logs.html.tmpl")),
		"stats":      template.Must(template.New("").ParseFS(t.fsys, "stats.html.tmpl")),
		"new":        template.Must(template.New("").ParseFS(t.fsys, "new.html.tmpl", "layout.html.tmpl")),
		"login":      template.Must(template.New("").ParseFS(t.fsys, "login.html.tmpl", "layout.html.tmpl")),
		"opensearch": template.Must(template.New("").ParseFS(t.fsys, "opensearch.xml.tmpl")),
	}
}

var errTemplateNotFound = errors.New("template not found")

func (t *Templates) RenderEcho(
	w io.Writer,
	name string,
	data interface{},
	c echo.Context,
) error {
	if t.dev {
		t.Must()
	}
	tmpl, ok := t.templates[name]
	if !ok {
		return errTemplateNotFound
	}
	layout := tmpl.Lookup("layout.html.tmpl")
	if layout == nil {
		return tmpl.ExecuteTemplate(w, name, data)
	}
	return tmpl.ExecuteTemplate(w, "layout", data)
}

func (t *Templates) Execute(
	w io.Writer,
	name string,
	data interface{},
) error {
	if t.dev {
		t.Must()
	}
	tmpl, ok := t.templates[name]
	if !ok {
		return errTemplateNotFound
	}
	return tmpl.ExecuteTemplate(w, name, data)
}
