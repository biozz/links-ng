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
		"index": template.Must(template.New("").ParseFS(t.fsys, "index.tmpl", "layout.tmpl")),
		"items": template.Must(template.New("").ParseFS(t.fsys, "items.tmpl")),
		"logs":  template.Must(template.New("").ParseFS(t.fsys, "logs.tmpl")),
		"stats": template.Must(template.New("").ParseFS(t.fsys, "stats.tmpl")),
		"new":   template.Must(template.New("").ParseFS(t.fsys, "new.tmpl", "layout.tmpl")),
		"login": template.Must(template.New("").ParseFS(t.fsys, "login.tmpl", "layout.tmpl")),
	}
}

var errTemplateNotFound = errors.New("template not found")

func (t *Templates) Render(
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
	layout := tmpl.Lookup("layout.tmpl")
	if layout == nil {
		return tmpl.ExecuteTemplate(w, name, data)
	}
	return tmpl.ExecuteTemplate(w, "layout", data)
}
