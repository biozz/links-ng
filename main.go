package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/biozz/links/web"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	_ "github.com/biozz/links/migrations"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/pocketbase/pocketbase/tools/types"
)

func main() {
	app := pocketbase.New()
	dev := strings.Contains(strings.Join(os.Args, " "), "--dev")
	tmpls := web.NewTemplates(dev)
	authMiddleware := &AuthMiddleware{app}

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		fsys, _ := fs.Sub(web.StaticFS, "static")

		se.Router.GET("/static/{path...}", apis.Static(fsys, false))

		se.Router.GET("/", func(e *core.RequestEvent) error {
			return tmpls.RenderEcho(e.Response, "index", nil, e)
		}).BindFunc(authMiddleware.Frontend)

		se.Router.GET("/new", func(e *core.RequestEvent) error {
			alias := e.Request.URL.Query().Get("alias")
			return tmpls.RenderEcho(e.Response, "new", alias, e)
		}).BindFunc(authMiddleware.Frontend)

		se.Router.POST("/items", func(e *core.RequestEvent) error {
			var newItem Item
			if err := e.BindBody(&newItem); err != nil {
				return e.String(http.StatusBadRequest, err.Error())
			}
			err := createItem(app, newItem)
			if err != nil {
				return e.String(http.StatusBadRequest, err.Error())
			}
			e.Response.Header().Set("HX-Redirect", "/")
			return e.String(http.StatusOK, "ok")
		}).BindFunc(authMiddleware.Frontend)

		se.Router.GET("/items", func(e *core.RequestEvent) error {
			q := e.Request.URL.Query().Get("q")
			var ctx ItemsContext
			itemsResult := getItems(app, q)
			ctx.Items = itemsResult.Items
			switch itemsResult.State {
			case NEW_ITEM:
				ctx.New = itemsResult.FirstQ
				return tmpls.RenderEcho(e.Response, "items", ctx, e)
			case ARGS_MODE:
				ctx.Expansion = itemsResult.Expansion
				return tmpls.RenderEcho(e.Response, "items", ctx, e)
			case GOOGLE_MODE:
				ctx.Expansion = itemsResult.Expansion
				ctx.IsGoogle = true
				return tmpls.RenderEcho(e.Response, "items", ctx, e)
			default:
				return tmpls.RenderEcho(e.Response, "items", ctx, e)
			}
		}).BindFunc(authMiddleware.Frontend)

		se.Router.GET("/logs", func(e *core.RequestEvent) error {
			logs := make([]Log, 0)
			query := app.DB().
				Select("id", "alias", "args", "created").
				From("logs")

			q := e.Request.URL.Query().Get("q")
			alias := strings.Split(q, " ")[0]
			if alias != "" {
				query = query.Where(dbx.HashExp{"alias": alias})
			}

			query.Limit(30).
				OrderBy("created DESC").
				All(&logs)

			logsContext := make([]LogContext, len(logs))
			for i, log := range logs {
				args := strings.Join(log.Args, " ")
				logsContext[i] = LogContext{
					Alias:     log.Alias,
					Args:      args,
					CreatedAt: log.CreatedAt.Time().Format("2006-01-02 15:04:05"),
				}
			}
			return tmpls.RenderEcho(e.Response, "logs", logsContext, e)
		}).BindFunc(authMiddleware.Frontend)

		se.Router.GET("/stats", func(e *core.RequestEvent) error {
			topN, _ := getTopAliases(app, 10)
			lowN, _ := getTopAliases(app, -10)
			result := make(map[string]any)
			result["topn"] = topN
			result["lown"] = lowN
			return tmpls.RenderEcho(e.Response, "stats", result, e)
		}).BindFunc(authMiddleware.Frontend)

		se.Router.GET("/help", func(e *core.RequestEvent) error {
			cookie, _ := e.Request.Cookie(COOKIE_NAME)
			deviceID := authMiddleware.GetDeviceIdFromCookie(e)
			ctx := HelpContext{
				DeviceID: deviceID,
				Token:    cookie.Value,
				AppURL:   app.Settings().Meta.AppURL,
			}
			return tmpls.RenderEcho(e.Response, "help", ctx, e)
		})

		se.Router.GET("/login", func(e *core.RequestEvent) error {
			return tmpls.RenderEcho(e.Response, "login", nil, e)
		})

		se.Router.POST("/login", func(e *core.RequestEvent) error {
			e.Request.ParseForm()
			deviceID := e.Request.FormValue("device_id")
			token := e.Request.FormValue("token")

			devices := []Device{}
			e.App.DB().
				NewQuery("SELECT id, token FROM devices WHERE id = {:device_id} AND token = {:token}").
				Bind(dbx.Params{
					"device_id": deviceID,
					"token":     token,
				}).
				All(&devices)

			if len(devices) != 1 {
				return e.String(http.StatusUnauthorized, "Invalid device ID or token")
			}

			cookie := new(http.Cookie)
			cookie.Name = COOKIE_NAME
			cookie.Value = token
			cookie.Expires = time.Now().Add(60 * 24 * time.Hour)
			e.SetCookie(cookie)
			e.Response.Header().Set("HX-Redirect", "/")
			return e.String(http.StatusOK, "ok")
		})

		se.Router.GET("/logout", func(e *core.RequestEvent) error {
			cookie := new(http.Cookie)
			cookie.Name = COOKIE_NAME
			cookie.Value = ""
			cookie.MaxAge = -1
			e.SetCookie(cookie)
			return e.Redirect(http.StatusTemporaryRedirect, "/login")
		})

		se.Router.GET("/nav", func(e *core.RequestEvent) error {
			deviceId := authMiddleware.GetDeviceIdFromCookie(e)
			ctx := NavContext{}
			if deviceId != "" {
				ctx.IsLoggedIn = true
			}
			return tmpls.RenderEcho(e.Response, "nav", ctx, e)
		})

		se.Router.GET("/expand/html", func(e *core.RequestEvent) error {
			q := e.Request.URL.Query().Get("q")

			itemsResult := getItems(app, q)
			switch itemsResult.State {
			case NEW_ITEM:
				e.Response.Header().Set("HX-Redirect", fmt.Sprintf("/new?alias=%s", itemsResult.FirstQ))
				return e.String(http.StatusOK, "ok")
			case GOOGLE_MODE:
				// This is a special shortcut
				createLog(app, itemsResult.Expansion.Alias, itemsResult.Expansion.Args, e.Get(DEVICE_ID_CONTEXT_KEY).(string))
				e.Response.Header().Set("HX-Redirect", itemsResult.Expansion.URL)
				return e.String(http.StatusOK, "ok")
			default:
				createLog(app, itemsResult.Expansion.Alias, itemsResult.Expansion.Args, e.Get(DEVICE_ID_CONTEXT_KEY).(string))
				e.Response.Header().Set("HX-Redirect", itemsResult.Expansion.URL)
				return e.String(http.StatusOK, "ok")
			}
		}).BindFunc(authMiddleware.Frontend)

		se.Router.GET("/api/items", func(e *core.RequestEvent) error {
			q := e.Request.URL.Query().Get("q")
			itemsResult := getItems(app, q)
			return e.JSON(http.StatusOK, itemsResult.Items)
		}).BindFunc(authMiddleware.API)

		se.Router.GET("/api/expand", func(e *core.RequestEvent) error {
			q := e.Request.URL.Query().Get("q")
			deviceId := e.Get(DEVICE_ID_CONTEXT_KEY).(string)
			itemsResult := getItems(app, q)
			switch itemsResult.State {
			case NEW_ITEM:
				return e.Redirect(http.StatusTemporaryRedirect, fmt.Sprintf("/new?alias=%s", itemsResult.FirstQ))
			case GOOGLE_MODE:
				// This is a special shortcut
				createLog(app, itemsResult.Expansion.Alias, itemsResult.Expansion.Args, deviceId)
				return e.Redirect(http.StatusTemporaryRedirect, itemsResult.Expansion.URL)
			default:
				createLog(app, itemsResult.Expansion.Alias, itemsResult.Expansion.Args, deviceId)
				return e.Redirect(http.StatusTemporaryRedirect, itemsResult.Expansion.URL)
			}
		}).BindFunc(authMiddleware.API)

		se.Router.GET("/api/opensearch", func(e *core.RequestEvent) error {
			// TODO: possibly parse `format` GET-parameter and output differently, i.e. in XML
			q := e.Request.URL.Query().Get("q")
			qParts := strings.Split(q, " ")
			itemsResult := getItems(app, q)
			suggestions := make([]string, len(itemsResult.Items))
			for i := 0; i < len(itemsResult.Items); i++ {
				expansion := expand(itemsResult.Items[i], q)
				suggestions[i] = fmt.Sprintf("%s %s %s", itemsResult.Items[i].Alias, qParts[:1], expansion.URL)
			}
			result := []any{
				q,
				suggestions,
				// This doesn't work, dunno why is it in a specification
				// https://github.com/dewitt/opensearch/blob/master/mediawiki/Specifications/OpenSearch/Extensions/Suggestions/1.1/Draft%201.wiki
				// []string{"description"},
				// []string{"https://google.com/?q=asdf"},
			}
			return e.JSON(http.StatusOK, result)
		}).BindFunc(authMiddleware.API)

		// New unified search endpoint
		se.Router.GET("/api/search", func(e *core.RequestEvent) error {
			q := e.Request.URL.Query().Get("q")
			action := e.Request.URL.Query().Get("action")
			deviceId := e.Get(DEVICE_ID_CONTEXT_KEY).(string)

			itemsResult := getItems(app, q)

			switch action {
			case "expand":
				switch itemsResult.State {
				case NEW_ITEM:
					return e.Redirect(http.StatusTemporaryRedirect, fmt.Sprintf("/new?alias=%s", itemsResult.FirstQ))
				case GOOGLE_MODE:
					// This is a special shortcut
					createLog(app, itemsResult.Expansion.Alias, itemsResult.Expansion.Args, deviceId)
					return e.Redirect(http.StatusTemporaryRedirect, itemsResult.Expansion.URL)
				default:
					createLog(app, itemsResult.Expansion.Alias, itemsResult.Expansion.Args, deviceId)
					return e.Redirect(http.StatusTemporaryRedirect, itemsResult.Expansion.URL)
				}
			case "suggest":
				qParts := strings.Split(q, " ")
				suggestions := make([]string, len(itemsResult.Items))
				for i := 0; i < len(itemsResult.Items); i++ {
					expansion := expand(itemsResult.Items[i], q)
					suggestions[i] = fmt.Sprintf("%s %s %s", itemsResult.Items[i].Alias, qParts[:1], expansion.URL)
				}
				result := []any{
					q,
					suggestions,
				}
				return e.JSON(http.StatusOK, result)
			default:
				return e.String(http.StatusBadRequest, "invalid action")
			}
		}).BindFunc(authMiddleware.API)

		se.Router.GET("/opensearch.xml", func(e *core.RequestEvent) error {
			// https://github.com/dewitt/opensearch/blob/master/opensearch-1-1-draft-6.md
			// TODO:
			// 	- add more response formats
			// 	<Url type="application/atom+xml" template="{{ .BaseURL }}/?q={searchTerms}&amp;format=atom"/>
			// 	<Url type="application/rss+xml" template="{{ .BaseURL }}/?q={searchTerms}&amp;pw={startPage?}&amp;format=rss"/>

			appUrl := app.Settings().Meta.AppURL
			var output bytes.Buffer
			err := tmpls.Execute(&output, "opensearch", map[string]string{"BaseURL": appUrl})
			if err != nil {
				return err
			}
			e.Response.Header().Set("Content-Type", "application/opensearchdescription+xml")
			return e.XML(http.StatusOK, output.Bytes())
			// not using AuthMiddleware, because Firefox can't download the search engine definition otherwise.
		})

		return se.Next()
	})

	isGoRun := strings.HasPrefix(os.Args[0], os.TempDir())

	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		// enable auto creation of migration files when making collection changes in the Admin UI
		// (the isGoRun check is to enable it only during development)
		Automigrate: isGoRun,
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

const (
	DEVICE_ID_CONTEXT_KEY = "device_id"
	COOKIE_NAME           = "links_auth"
)

type Item struct {
	ID           string   `db:"id" json:"id"`
	CollectionID string   `json:"collection_id"`
	Name         string   `db:"name" form:"name" json:"name"`
	Alias        string   `db:"alias" form:"alias" json:"alias"`
	URL          string   `db:"url" form:"url" json:"url"`
	Tags         []string `form:"tags" json:"tags"`
}

type Expansion struct {
	Alias     string
	Args      []string
	URL       string
	ExpandURL string
}

type ItemsContext struct {
	New       string
	Expansion Expansion
	Items     []Item
	IsGoogle  bool
}

type Log struct {
	ID        string                  `db:"id"`
	Alias     string                  `db:"alias"`
	Args      types.JSONArray[string] `db:"args"`
	CreatedAt types.DateTime          `db:"created"`
}

type LogContext struct {
	Alias     string
	Args      string
	CreatedAt string
}

type NavContext struct {
	IsLoggedIn bool
}

type HelpContext struct {
	DeviceID string
	Token    string
	AppURL   string
}

func expand(item Item, q string) Expansion {
	url := item.URL
	substCount := strings.Count(url, "%s")
	qParts := strings.SplitN(q, " ", substCount+1)
	args := make([]string, len(qParts)-1)
	// First element is search prefix, we don't need that
	for i := 1; i < len(qParts); i++ {
		if strings.Contains(url, "%s") {
			args[i-1] = qParts[i]
			url = strings.Replace(url, "%s", qParts[i], 1)
		}
	}
	return Expansion{
		Alias: item.Alias,
		Args:  args,
		URL:   url,
	}
}

func getItemsByPrefix(app *pocketbase.PocketBase, prefix string) []Item {
	items := make([]Item, 0)
	app.DB().
		NewQuery("SELECT alias, name, url FROM items WHERE alias LIKE {:like} ORDER BY (CASE WHEN alias = {:prefix} THEN 1 WHEN alias LIKE {:like} THEN 2 ELSE 3 END), alias, created ASC LIMIT 10").
		Bind(dbx.Params{
			"prefix": prefix,
			"like":   prefix + "%",
		}).
		All(&items)
	return items
}

func getItemsByExactMatch(app *pocketbase.PocketBase, alias string) []Item {
	items := make([]Item, 0)
	app.DB().
		NewQuery("SELECT alias, name, url FROM items WHERE alias = {:alias}").
		Bind(dbx.Params{
			"alias": alias,
		}).
		All(&items)
	return items
}

func createItem(app *pocketbase.PocketBase, item Item) error {
	collection, err := app.FindCollectionByNameOrId("items")
	if err != nil {
		return err
	}
	record := core.NewRecord(collection)
	record.Set("name", item.Name)
	record.Set("alias", item.Alias)
	record.Set("url", item.URL)
	record.Set("tags", item.Tags)
	if err := app.Save(record); err != nil {
		return err
	}
	return nil
}

func createLog(app *pocketbase.PocketBase, alias string, args []string, deviceId string) error {
	collection, err := app.FindCollectionByNameOrId("logs")
	if err != nil {
		return err
	}
	record := core.NewRecord(collection)
	record.Set("alias", alias)
	record.Set("args", args)
	record.Set("device", deviceId)
	if err := app.Save(record); err != nil {
		return err
	}
	return nil
}

type TopAlias struct {
	Alias string `db:"alias"`
	Count int64  `db:"count"`
}

func getTopAliases(app *pocketbase.PocketBase, limit int64) ([]TopAlias, error) {
	order := "DESC"
	if limit < 0 {
		limit = -limit
		order = "ASC"
	}
	aliases := make([]TopAlias, 0)
	app.DB().
		Select("alias", "count(*) as count").
		From("logs").
		GroupBy("alias").
		OrderBy("count(*)" + order).
		AndOrderBy("created ASC").
		Limit(limit).
		All(&aliases)
	return aliases, nil
}

type ItemsState uint8

const (
	UNKNOWN        ItemsState = 0
	MULTIPLE_ITEMS ItemsState = 1
	NEW_ITEM       ItemsState = 2
	ARGS_MODE      ItemsState = 3
	GOOGLE_MODE    ItemsState = 4
)

type ItemsResult struct {
	State     ItemsState
	Items     []Item
	Expansion Expansion
	FirstQ    string
}

func getItems(app *pocketbase.PocketBase, q string) ItemsResult {
	appURL := app.Settings().Meta.AppURL
	// q is a space separated alias with parameters, which has to be split into
	// certain number of parts, which are replaced in %s in the URL
	// For example, q can be `g test`. `g` is an alias and `test` is a parameter.
	qParts := strings.Split(q, " ")
	result := ItemsResult{
		State:     UNKNOWN,
		Expansion: Expansion{},
		Items:     []Item{},
		FirstQ:    qParts[0],
	}

	var items []Item

	if len(qParts) > 1 {
		items = getItemsByExactMatch(app, qParts[0])
	} else {
		// Fisrt element of the query is ~~almost~~ always an alias prefix
		items = getItemsByPrefix(app, qParts[0])
	}

	if len(items) == 0 {
		if len(qParts) > 1 {
			result.State = GOOGLE_MODE
			googleQ := strings.Join(qParts, " ")
			if qParts[0] == "g" {
				googleQ = strings.Join(qParts[1:], " ")
			}
			googleQ = url.QueryEscape(googleQ)
			result.Expansion = Expansion{
				Alias:     "g",
				Args:      qParts[1:],
				URL:       "https://google.com/search?q=" + googleQ,
				ExpandURL: fmt.Sprintf("%s/api/search?q=%s&action=expand", appURL, googleQ),
			}
			return result
		}
		result.State = NEW_ITEM
		return result
	}

	result.Items = items
	result.State = MULTIPLE_ITEMS
	result.Expansion = expand(items[0], q)

	if len(qParts) > 1 && len(items) > 0 {
		result.State = ARGS_MODE
		result.Expansion = expand(items[0], q)
		result.Expansion.ExpandURL = fmt.Sprintf("%s/api/search?q=%s&action=expand", appURL, q)
		return result
	}

	return result
}

type AuthMiddleware struct {
	app *pocketbase.PocketBase
}

type Device struct {
	ID    string `db:"id"`
	Token string `db:"token"`
}

func (m *AuthMiddleware) GetDeviceIdFromCookie(e *core.RequestEvent) string {
	cookie, err := e.Request.Cookie(COOKIE_NAME)
	if err != nil {
		return ""
	}
	devices := []Device{}
	m.app.DB().
		NewQuery("SELECT id FROM devices WHERE token = {:token}").
		Bind(dbx.Params{
			"token": cookie.Value,
		}).
		All(&devices)
	if len(devices) != 1 {
		return ""
	}
	return devices[0].ID
}

func (m *AuthMiddleware) Frontend(e *core.RequestEvent) error {
	deviceId := m.GetDeviceIdFromCookie(e)
	if deviceId == "" {
		return e.Redirect(http.StatusTemporaryRedirect, "/login")
	}
	e.Set(DEVICE_ID_CONTEXT_KEY, deviceId)
	return e.Next()
}

func (m *AuthMiddleware) API(e *core.RequestEvent) error {
	token := e.Request.URL.Query().Get("t")
	deviceID := e.Request.URL.Query().Get("id")
	if token == "" || deviceID == "" {
		return e.String(http.StatusUnauthorized, "missing token or id")
	}
	devices := []Device{}
	m.app.DB().
		NewQuery("SELECT id, token FROM devices WHERE id = {:id} AND token = {:token}").
		Bind(dbx.Params{
			"id":    deviceID,
			"token": token,
		}).
		All(&devices)
	if len(devices) != 1 {
		return e.String(http.StatusUnauthorized, "invalid token or id")
	}
	e.Set(DEVICE_ID_CONTEXT_KEY, devices[0].ID)
	return e.Next()
}
