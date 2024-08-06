package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/biozz/links/web"
	"github.com/labstack/echo/v5"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/pocketbase/pocketbase/tools/types"

	_ "github.com/biozz/links/migrations"
)

func main() {
	pb := pocketbase.New()
	dev := strings.Contains(strings.Join(os.Args, " "), "--dev")
	tmpls := web.NewTemplates(dev)
	authMiddleware := &AuthMiddleware{pb}

	pb.OnBeforeServe().Add(func(e *core.ServeEvent) error {
		fsys, _ := fs.Sub(web.StaticFS, "static")

		e.Router.GET("/static/*", apis.StaticDirectoryHandler(fsys, false))

		e.Router.GET("/", func(c echo.Context) error {
			return tmpls.Render(c.Response().Writer, "index", nil, c)
		}, authMiddleware.Process)

		e.Router.GET("/new", func(c echo.Context) error {
			return tmpls.Render(c.Response().Writer, "new", c.Request().URL.Query().Get("alias"), c)
		}, authMiddleware.Process)

		e.Router.POST("/items", func(c echo.Context) error {
			var newItem Item
			if err := c.Bind(&newItem); err != nil {
				return c.String(http.StatusBadRequest, err.Error())
			}
			err := createItem(pb, newItem)
			if err != nil {
				return c.String(http.StatusBadRequest, err.Error())
			}
			c.Response().Header().Set("HX-Redirect", "/")
			return c.String(http.StatusOK, "ok")
		}, authMiddleware.Process)

		e.Router.GET("/items", func(c echo.Context) error {
			q := c.Request().URL.Query().Get("q")
			var ctx ItemsContext
			itemsResult := getItems(pb, q)
			ctx.Items = itemsResult.Items
			switch itemsResult.State {
			case NEW_ITEM:
				ctx.New = itemsResult.FirstQ
				return tmpls.Render(c.Response().Writer, "items", ctx, c)
			case ARGS_MODE:
				ctx.Expansion = itemsResult.Expansion
				return tmpls.Render(c.Response().Writer, "items", ctx, c)
			default:
				return tmpls.Render(c.Response().Writer, "items", ctx, c)
			}
		}, authMiddleware.Process)

		e.Router.GET("/logs", func(c echo.Context) error {
			logs := make([]Log, 0)
			pb.Dao().DB().
				Select("id", "alias", "args", "created").
				From("logs").
				Limit(30).
				OrderBy("created DESC").
				All(&logs)
			logsContext := make([]LogContext, len(logs))
			for i, log := range logs {
				logsContext[i] = LogContext{
					Alias:     log.Alias,
					Args:      strings.Join(log.Args, " "),
					CreatedAt: log.CreatedAt.Time().Format("2006-01-02 15:04:05"),
				}
			}
			return tmpls.Render(c.Response().Writer, "logs", logsContext, c)
		}, authMiddleware.Process)

		e.Router.GET("/stats", func(c echo.Context) error {
			topN, _ := getTopAliases(pb, 10)
			lowN, _ := getTopAliases(pb, -10)
			result := make(map[string]interface{})
			result["topn"] = topN
			result["lown"] = lowN
			return tmpls.Render(c.Response().Writer, "stats", result, c)
		}, authMiddleware.Process)

		e.Router.GET("/expand/html", func(c echo.Context) error {
			q := c.QueryParam("q")

			itemsResult := getItems(pb, q)
			switch itemsResult.State {
			case NEW_ITEM:
				c.Response().Header().Set("HX-Redirect", fmt.Sprintf("/new?alias=%s", itemsResult.FirstQ))
				return c.String(http.StatusOK, "ok")
			case GOOGLE_MODE:
				// This is a special shortcut
				createLog(pb, itemsResult.Expansion.Alias, itemsResult.Expansion.Args, c.Get(DEVICE_ID_CONTEXT_KEY).(string))
				c.Response().Header().Set("HX-Redirect", itemsResult.Expansion.URL)
				return c.String(http.StatusOK, "ok")
			default:
				createLog(pb, itemsResult.Expansion.Alias, itemsResult.Expansion.Args, c.Get(DEVICE_ID_CONTEXT_KEY).(string))
				c.Response().Header().Set("HX-Redirect", itemsResult.Expansion.URL)
				return c.String(http.StatusOK, "ok")
			}
		}, authMiddleware.Process)

		e.Router.GET("/api/expand", func(c echo.Context) error {
			q := c.QueryParam("q")
			itemsResult := getItems(pb, q)
			switch itemsResult.State {
			case NEW_ITEM:
				return c.Redirect(http.StatusTemporaryRedirect, fmt.Sprintf("/new?alias=%s", itemsResult.FirstQ))
			case GOOGLE_MODE:
				// This is a special shortcut
				createLog(pb, itemsResult.Expansion.Alias, itemsResult.Expansion.Args, c.Get(DEVICE_ID_CONTEXT_KEY).(string))
				return c.Redirect(http.StatusTemporaryRedirect, itemsResult.Expansion.URL)
			default:
				createLog(pb, itemsResult.Expansion.Alias, itemsResult.Expansion.Args, c.Get(DEVICE_ID_CONTEXT_KEY).(string))
				return c.Redirect(http.StatusTemporaryRedirect, itemsResult.Expansion.URL)
			}
		}, authMiddleware.Process)

		e.Router.GET("/api/opensearch", func(c echo.Context) error {
			q := c.QueryParam("q")
			qParts := strings.Split(q, " ")
			itemsResult := getItems(pb, q)
			suggestions := make([]string, len(itemsResult.Items))
			for i := 0; i < len(itemsResult.Items); i++ {
				expansion := expand(itemsResult.Items[i], q)
				suggestions[i] = fmt.Sprintf("%s %s %s", itemsResult.Items[i].Alias, qParts[:1], expansion.URL)
			}
			result := []interface{}{
				q,
				suggestions,
				// This doesn't work, dunno why is it in a specification
				// https://github.com/dewitt/opensearch/blob/master/mediawiki/Specifications/OpenSearch/Extensions/Suggestions/1.1/Draft%201.wiki
				// []string{"description"},
				// []string{"https://google.com/?q=asdf"},
			}
			return c.JSON(http.StatusOK, result)
		}, authMiddleware.Process)

		e.Router.GET("/opensearch.xml", func(c echo.Context) error {
			appUrl := pb.Settings().Meta.AppUrl
			tmpl, err := template.New("opensearch.xml").Parse(opensearchXML)
			if err != nil {
				return err
			}
			var output bytes.Buffer
			if err := tmpl.Execute(&output, map[string]string{"BaseURL": appUrl}); err != nil {
				return err
			}
			c.Response().Header().Set("Content-Type", "application/opensearchdescription+xml")
			return c.Blob(http.StatusOK, "application/octet-stream", output.Bytes())
		}, authMiddleware.Process)

		e.Router.GET("/login", func(c echo.Context) error {
			return tmpls.Render(c.Response().Writer, "login", nil, c)
		})

		e.Router.POST("/login", func(c echo.Context) error {
			c.Request().ParseForm()
			cookie := new(http.Cookie)
			cookie.Name = COOKIE_NAME
			cookie.Value = c.FormValue("token")
			cookie.Expires = time.Now().Add(60 * 24 * time.Hour)
			c.SetCookie(cookie)
			c.Response().Header().Set("HX-Redirect", "/")
			return c.String(http.StatusOK, "ok")
		})

		return nil
	})

	isGoRun := strings.HasPrefix(os.Args[0], os.TempDir())

	migratecmd.MustRegister(pb, pb.RootCmd, migratecmd.Config{
		// enable auto creation of migration files when making collection changes in the Admin UI
		// (the isGoRun check is to enable it only during development)
		Automigrate: isGoRun,
	})

	if err := pb.Start(); err != nil {
		log.Fatal(err)
	}
}

const (
	DEVICE_ID_CONTEXT_KEY = "device_id"
	COOKIE_NAME           = "links_auth"
)

type Item struct {
	Name  string   `db:"name" form:"name"`
	Alias string   `db:"alias" form:"alias"`
	URL   string   `db:"url" form:"url"`
	Tags  []string `form:"tags"`
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
}

type Log struct {
	ID        string                  `db:"id"`
	Alias     string                  `db:"alias"`
	Args      types.JsonArray[string] `db:"args"`
	CreatedAt types.DateTime          `db:"created"`
}

type LogContext struct {
	Alias     string
	Args      string
	CreatedAt string
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

func getItemsByPrefix(pb *pocketbase.PocketBase, prefix string) []Item {
	items := make([]Item, 0)
	pb.Dao().DB().
		NewQuery("SELECT alias, name, url FROM items WHERE alias LIKE {:like} ORDER BY alias LIKE {:like}, created ASC LIMIT 10").
		Bind(dbx.Params{
			"like": prefix + "%",
		}).
		All(&items)
	return items
}

func getItemsByExactMatch(pb *pocketbase.PocketBase, alias string) []Item {
	items := make([]Item, 0)
	pb.Dao().DB().
		NewQuery("SELECT alias, name, url FROM items WHERE alias = {:alias}").
		Bind(dbx.Params{
			"alias": alias,
		}).
		All(&items)
	return items
}

func createItem(pb *pocketbase.PocketBase, item Item) error {
	collection, err := pb.Dao().FindCollectionByNameOrId("items")
	if err != nil {
		return err
	}
	record := models.NewRecord(collection)
	record.Set("name", item.Name)
	record.Set("alias", item.Alias)
	record.Set("url", item.URL)
	record.Set("tags", item.Tags)
	if err := pb.Dao().SaveRecord(record); err != nil {
		return err
	}
	return nil
}

func createLog(pb *pocketbase.PocketBase, alias string, args []string, deviceId string) error {
	collection, err := pb.Dao().FindCollectionByNameOrId("logs")
	if err != nil {
		return err
	}
	record := models.NewRecord(collection)
	record.Set("alias", alias)
	record.Set("args", args)
	record.Set("device", deviceId)
	if err := pb.Dao().SaveRecord(record); err != nil {
		return err
	}
	return nil
}

type TopAlias struct {
	Alias string `db:"alias"`
	Count int64  `db:"count"`
}

func getTopAliases(pb *pocketbase.PocketBase, limit int64) ([]TopAlias, error) {
	order := "DESC"
	if limit < 0 {
		limit = -limit
		order = "ASC"
	}
	aliases := make([]TopAlias, 0)
	pb.Dao().DB().
		Select("alias", "count(*) as count").
		From("logs").
		GroupBy("alias").
		OrderBy("count(*)" + order).
		AndOrderBy("created ASC").
		Limit(limit).
		All(&aliases)
	return aliases, nil
}

// https://github.com/dewitt/opensearch/blob/master/opensearch-1-1-draft-6.md
var opensearchXML string = `
<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/"
                       xmlns:moz="http:/www.mozilla.org/2006/browser/search/">
  <ShortName>Links</ShortName>
  <InputEncoding>UTF-8</InputEncoding>
  <Description>Just a bunch of links.</Description>
  <Tags>links</Tags>
  <Contact>ielfimov@gmail.com</Contact>
  <Url type="text/html" method="get" template="{{ .BaseURL }}/api/expand?q={searchTerms}" />
  <Url type="application/x-suggestions+json" rel="suggestions" template="{{ .BaseURL }}/api/opensearch?q={searchTerms}" />
  <moz:SearchForm>{{ .BaseURL }}</moz:SearchForm>
</OpenSearchDescription>
`

type ItemsState uint8

const (
	UNKNOWN        ItemsState = 0
	MULTIPLE_ITEMS            = 1
	NEW_ITEM                  = 2
	ARGS_MODE                 = 3
	GOOGLE_MODE               = 4
)

type ItemsResult struct {
	State     ItemsState
	Items     []Item
	Expansion Expansion
	FirstQ    string
}

func getItems(pb *pocketbase.PocketBase, q string) ItemsResult {
	appURL := pb.Settings().Meta.AppUrl
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
		items = getItemsByExactMatch(pb, qParts[0])
	} else {
		// Fisrt element of the query is ~~almost~~ always an alias prefix
		items = getItemsByPrefix(pb, qParts[0])
	}

	result.Items = items
	result.State = MULTIPLE_ITEMS
	result.Expansion = expand(items[0], q)

	if len(items) == 0 {
		if len(qParts) > 1 {
			result.State = GOOGLE_MODE
			result.Expansion = Expansion{
				Alias:     "g",
				Args:      qParts[1:],
				URL:       "https://google.com/search?q=" + strings.Join(qParts[1:], "+"),
				ExpandURL: fmt.Sprintf("%s/api/expand?q=%s", appURL, q),
			}
			return result
		}
		result.State = NEW_ITEM
		return result
	}

	if len(qParts) > 1 && len(items) > 0 {
		result.State = ARGS_MODE
		result.Expansion = expand(items[0], q)
		result.Expansion.ExpandURL = fmt.Sprintf("%s/api/expand?q=%s", appURL, q)
		return result
	}

	return result
}

type AuthMiddleware struct {
	pb *pocketbase.PocketBase
}

type Device struct {
	ID    string `db:"id"`
	Token string `db:"token"`
}

func (m *AuthMiddleware) Process(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		cookie, err := c.Cookie(COOKIE_NAME)
		if err != nil {
			return c.String(http.StatusOK, "")
		}
		devices := []Device{}
		m.pb.Dao().DB().
			NewQuery("SELECT id, token FROM devices WHERE token = {:token}").
			Bind(dbx.Params{
				"token": cookie.Value,
			}).
			All(&devices)
		fmt.Println(devices)
		if len(devices) != 1 {
			return c.String(http.StatusOK, "")
		}
		c.Set(DEVICE_ID_CONTEXT_KEY, devices[0].ID)
		return next(c)
	}
}
