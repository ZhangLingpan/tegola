package postgis

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v4/pgxpool"

	"github.com/go-spatial/tegola"
	"github.com/go-spatial/tegola/internal/ttools"
	"github.com/go-spatial/tegola/provider"
)

func TestReplaceTokens(t *testing.T) {
	type tcase struct {
		sql      string
		tile     provider.Tile
		expected string
		layer    Layer
	}

	fn := func(tc tcase) func(t *testing.T) {
		return func(t *testing.T) {
			sql, err := replaceTokens(tc.sql, &tc.layer, tc.tile, true)
			if err != nil {
				t.Errorf("unexpected error, Expected nil Got %v", err)
				return
			}

			if sql != tc.expected {
				t.Errorf("incorrect sql,\n Expected \n \t%v\n Got \n \t%v", tc.expected, sql)
				return
			}
		}
	}

	tests := map[string]tcase{
		"replace BBOX": {
			sql:      "SELECT * FROM foo WHERE geom && !BBOX!",
			layer:    Layer{srid: tegola.WebMercator},
			tile:     provider.NewTile(2, 1, 1, 64, tegola.WebMercator),
			expected: "SELECT * FROM foo WHERE geom && ST_MakeEnvelope(-1.017529720390625e+07,-156543.03390625,156543.03390625,1.017529720390625e+07,3857)",
		},
		"replace BBOX with != in query": {
			sql:      "SELECT * FROM foo WHERE geom && !BBOX! AND bar != 42",
			layer:    Layer{srid: tegola.WebMercator},
			tile:     provider.NewTile(2, 1, 1, 64, tegola.WebMercator),
			expected: "SELECT * FROM foo WHERE geom && ST_MakeEnvelope(-1.017529720390625e+07,-156543.03390625,156543.03390625,1.017529720390625e+07,3857) AND bar != 42",
		},
		"replace BBOX and ZOOM 1": {
			sql:      "SELECT id, scalerank=!ZOOM! FROM foo WHERE geom && !BBOX!",
			layer:    Layer{srid: tegola.WebMercator},
			tile:     provider.NewTile(2, 1, 1, 64, tegola.WebMercator),
			expected: "SELECT id, scalerank=2 FROM foo WHERE geom && ST_MakeEnvelope(-1.017529720390625e+07,-156543.03390625,156543.03390625,1.017529720390625e+07,3857)",
		},
		"replace BBOX and ZOOM 2": {
			sql:      "SELECT id, scalerank=!ZOOM! FROM foo WHERE geom && !BBOX!",
			layer:    Layer{srid: tegola.WebMercator},
			tile:     provider.NewTile(16, 11241, 26168, 64, tegola.WebMercator),
			expected: "SELECT id, scalerank=16 FROM foo WHERE geom && ST_MakeEnvelope(-1.3163688815956049e+07,4.0352540420407765e+06,-1.3163058210472783e+07,4.035884647524042e+06,3857)",
		},
		"replace pixel_width/height and scale_denominator": {
			sql:      "SELECT id, !pixel_width! as width, !pixel_height! as height, !scale_denominator! as scale_denom FROM foo WHERE geom && !BBOX!",
			layer:    Layer{srid: tegola.WebMercator},
			tile:     provider.NewTile(11, 1070, 676, 64, tegola.WebMercator),
			expected: "SELECT id, 76.43702827453671 as width, 76.43702827453671 as height, 272989.38669477403 as scale_denom FROM foo WHERE geom && ST_MakeEnvelope(899816.6968478388,6.789748347570495e+06,919996.0723123164,6.809927723034973e+06,3857)",
		},
	}

	for name, tc := range tests {
		t.Run(name, fn(tc))
	}
}

func TestReplaceParams(t *testing.T) {
	type tcase struct {
		params       map[string]provider.QueryParameter
		sql          string
		expectedSql  string
		expectedArgs []interface{}
	}

	fn := func(tc tcase) func(t *testing.T) {
		return func(t *testing.T) {
			args := make([]interface{}, 0)
			out := provider.ReplaceParams(tc.params, tc.sql, &args)

			if out != tc.expectedSql {
				t.Errorf("expected \n \t%v\n out \n \t%v", tc.expectedSql, out)
				return
			}

			if len(tc.expectedArgs) != len(args) {
				t.Errorf("expected \n \t%v\n out \n \t%v", tc.expectedArgs, args)
				return
			}
			for i, arg := range tc.expectedArgs {
				if arg != args[i] {
					t.Errorf("expected \n \t%v\n out \n \t%v", tc.expectedArgs, args)
					return
				}
			}
		}
	}

	tests := map[string]tcase{
		"nil params": {
			params:       nil,
			sql:          "SELECT * FROM table",
			expectedSql:  "SELECT * FROM table",
			expectedArgs: []interface{}{},
		},
		"int replacement": {
			params: map[string]provider.QueryParameter{
				"!PARAM!": {
					Token: "!PARAM!",
					SQL:   "?",
					Value: 1,
				},
			},
			sql:          "SELECT * FROM table WHERE PARAM = !PARAM!",
			expectedSql:  "SELECT * FROM table WHERE PARAM = $1",
			expectedArgs: []interface{}{1},
		},
		"string replacement": {
			params: map[string]provider.QueryParameter{
				"!PARAM!": {
					Token: "!PARAM!",
					SQL:   "?",
					Value: "test",
				},
			},
			sql:          "SELECT * FROM table WHERE PARAM = !PARAM!",
			expectedSql:  "SELECT * FROM table WHERE PARAM = $1",
			expectedArgs: []interface{}{"test"},
		},
		"null replacement": {
			params: map[string]provider.QueryParameter{
				"!PARAM!": {
					Token: "!PARAM!",
					SQL:   "?",
					Value: nil,
				},
			},
			sql:          "SELECT * FROM table WHERE PARAM = !PARAM!",
			expectedSql:  "SELECT * FROM table WHERE PARAM = $1",
			expectedArgs: []interface{}{nil},
		},
		"complex sql replacement": {
			params: map[string]provider.QueryParameter{
				"!PARAM!": {
					Token: "!PARAM!",
					SQL:   "WHERE PARAM=?",
					Value: 1,
				},
			},
			sql:          "SELECT * FROM table !PARAM!",
			expectedSql:  "SELECT * FROM table WHERE PARAM=$1",
			expectedArgs: []interface{}{1},
		},
		"subquery removal": {
			params: map[string]provider.QueryParameter{
				"!PARAM!": {
					Token: "!PARAM!",
					SQL:   "",
					Value: nil,
				},
			},
			sql:          "SELECT * FROM table !PARAM!",
			expectedSql:  "SELECT * FROM table ",
			expectedArgs: []interface{}{},
		},
	}

	for name, tc := range tests {
		t.Run(name, fn(tc))
	}
}

func TestUppercaseTokens(t *testing.T) {
	type tcase struct {
		str      string
		expected string
	}

	fn := func(tc tcase) func(t *testing.T) {
		return func(t *testing.T) {
			out := uppercaseTokens(tc.str)

			if out != tc.expected {
				t.Errorf("expected \n \t%v\n out \n \t%v", tc.expected, out)
				return
			}
		}
	}

	tests := map[string]tcase{
		"uppercase tokens": {
			str:      "this !lower! case !STrInG! should uppercase !TOKENS!",
			expected: "this !LOWER! case !STRING! should uppercase !TOKENS!",
		},
		"no tokens": {
			str:      "no token",
			expected: "no token",
		},
		"empty string": {
			str:      "",
			expected: "",
		},
		"unclosed token": {
			str:      "unclosed !token",
			expected: "unclosed !token",
		},
	}

	for name, tc := range tests {
		t.Run(name, fn(tc))
	}
}

func TestDecipherFields(t *testing.T) {
	ttools.ShouldSkip(t, TESTENV)

	type tcase struct {
		sql              string
		expectedRowCount int
		expectedTags     map[string]interface{}
	}

	host := ttools.GetEnvDefault("PGHOST", "localhost")
	port, err := strconv.Atoi(ttools.GetEnvDefault("PGPORT", "5432"))
	// if port is anything but int, fallback to default
	if err != nil {
		port = 5432
	}
	db := ttools.GetEnvDefault("PGDATABASE", "tegola")
	user := ttools.GetEnvDefault("PGUSER", "postgres")
	password := ttools.GetEnvDefault("PGPASSWORD", "postgres")

	cs := fmt.Sprintf("postgres://%v:%v@%v:%v/%v", user, password, host, port, db)
	dbconfig, err := BuildDBConfig(cs)

	if err != nil {
		t.Fatalf("unable to build db config: %v", err)
	}

	conn, err := pgxpool.ConnectConfig(context.Background(), dbconfig)
	if err != nil {
		t.Fatalf("unable to connect to database: %v", err)
	}
	defer conn.Close()

	fn := func(tc tcase) func(t *testing.T) {
		return func(t *testing.T) {
			rows, err := conn.Query(context.Background(), tc.sql)
			if err != nil {
				t.Errorf("Error performing query: %v", err)
				return
			}
			defer rows.Close()

			var rowCount int
			for rows.Next() {
				geoFieldname := "geom"
				idFieldname := "id"
				descriptions := rows.FieldDescriptions()

				vals, err := rows.Values()
				if err != nil {
					t.Errorf("unexepcted error reading row Values: %v", err)
					return
				}

				_, _, tags, err := decipherFields(context.TODO(), geoFieldname, idFieldname, descriptions, vals)
				if err != nil {
					t.Errorf("unexepcted error running decipherFileds: %v", err)
					return
				}

				if len(tags) != len(tc.expectedTags) {
					t.Errorf("got %v tags, expecting %v: %#v, %#v", len(tags), len(tc.expectedTags), tags, tc.expectedTags)
					return
				}

				for k, v := range tags {
					if tc.expectedTags[k] != v {
						t.Errorf("missing or bad value for tag %v: %v (%T) != %v (%T)", k, v, v, tc.expectedTags[k], tc.expectedTags[k])
						return
					}
				}

				rowCount++
			}
			if rows.Err() != nil {
				t.Errorf("unexpected err: %v", rows.Err())
				return
			}

			if rowCount != tc.expectedRowCount {
				t.Errorf("invalid row count. expected %v. got %v", tc.expectedRowCount, rowCount)
				return
			}
		}
	}

	tests := map[string]tcase{
		"hstore 1": {
			sql:              "SELECT id, tags, int8_test FROM hstore_test WHERE id = 1;",
			expectedRowCount: 1,
			expectedTags: map[string]interface{}{
				"height":    "9",
				"int8_test": int64(1000888),
			},
		},
		"hstore 2": {
			sql:              "SELECT id, tags, int8_test FROM hstore_test WHERE id = 2;",
			expectedRowCount: 1,
			expectedTags: map[string]interface{}{
				"hello":     "there",
				"good":      "day",
				"int8_test": int64(8880001),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, fn(tc))
	}
}
