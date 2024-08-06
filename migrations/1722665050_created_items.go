package migrations

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/daos"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/models"
)

func init() {
	m.Register(func(db dbx.Builder) error {
		jsonData := `{
			"id": "39spxoreezeamnc",
			"created": "2024-08-03 06:04:10.950Z",
			"updated": "2024-08-03 06:04:10.950Z",
			"name": "items",
			"type": "base",
			"system": false,
			"schema": [
				{
					"system": false,
					"id": "jkylt5tw",
					"name": "name",
					"type": "text",
					"required": false,
					"presentable": false,
					"unique": false,
					"options": {
						"min": null,
						"max": null,
						"pattern": ""
					}
				},
				{
					"system": false,
					"id": "vhlj4tyu",
					"name": "url",
					"type": "text",
					"required": false,
					"presentable": false,
					"unique": false,
					"options": {
						"min": null,
						"max": null,
						"pattern": ""
					}
				},
				{
			        "system": false,
			        "id": "8efxazc3",
			        "name": "tags",
			        "type": "json",
			        "required": false,
			        "presentable": false,
			        "unique": false,
			        "options": {
			        	"maxSize": 2000000
			        }
				},
				{
					"system": false,
					"id": "7gqkvgvc",
					"name": "alias",
					"type": "text",
					"required": false,
					"presentable": false,
					"unique": false,
					"options": {
						"min": null,
						"max": null,
						"pattern": ""
					}
				}
			],
			"indexes": [],
			"listRule": null,
			"viewRule": null,
			"createRule": null,
			"updateRule": null,
			"deleteRule": null,
			"options": {}
		}`

		collection := &models.Collection{}
		if err := json.Unmarshal([]byte(jsonData), &collection); err != nil {
			return err
		}

		return daos.New(db).SaveCollection(collection)
	}, func(db dbx.Builder) error {
		dao := daos.New(db)

		collection, err := dao.FindCollectionByNameOrId("39spxoreezeamnc")
		if err != nil {
			return err
		}

		return dao.DeleteCollection(collection)
	})
}
