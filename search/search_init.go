package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/gao66666/GoBlog/models"
	"go.uber.org/zap"
)

var (
	client    *elasticsearch.Client
	indexName string
)

func Enabled() bool {
	return client != nil && indexName != ""
}

func Init(host, apiKey, index string) {
	if index == "" {
		index = "articles"
	}

	cfg := elasticsearch.Config{
		Addresses: []string{host},
		APIKey:    apiKey,
	}

	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		zap.L().Fatal("Elasticsearch client init failed", zap.Error(err))
	}

	// 简单连通性验证
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := es.Info(es.Info.WithContext(ctx))
	if err != nil {
		zap.L().Fatal("Elasticsearch 连接失败", zap.Error(err))
	}
	_ = res.Body.Close()

	client = es
	indexName = index
	ensureIndex()
	ensureCNFriendlySubfields()
}

func ensureIndex() {
	if !Enabled() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	existsRes, err := client.Indices.Exists([]string{indexName}, client.Indices.Exists.WithContext(ctx))
	if err == nil {
		defer existsRes.Body.Close()
		if existsRes.StatusCode == 200 {
			return
		}
	}

	// text + keyword 子字段：wildcard 可在整段标题/摘要上做子串匹配，弥补 standard 分词对中文不友好的问题。
	mapping := `{
	  "mappings": {
	    "properties": {
	      "id": {"type": "keyword"},
	      "title": {
	        "type": "text",
	        "fields": {
	          "raw": {"type": "keyword", "ignore_above": 4096}
	        }
	      },
	      "author_name": {"type": "text"},
	      "tags": {"type": "keyword"},
	      "summary": {
	        "type": "text",
	        "fields": {
	          "raw": {"type": "keyword", "ignore_above": 8192}
	        }
	      }
	    }
	  }
	}`

	createRes, err := client.Indices.Create(indexName, client.Indices.Create.WithContext(ctx), client.Indices.Create.WithBody(strings.NewReader(mapping)))
	if err != nil {
		zap.L().Warn("Elasticsearch create index failed", zap.String("index", indexName), zap.Error(err))
		return
	}
	_ = createRes.Body.Close()
}

// ensureCNFriendlySubfields 为已存在的索引补充 title.raw / summary.raw（仅 additive，失败则忽略）。
func ensureCNFriendlySubfields() {
	if !Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body := `{
	  "properties": {
	    "title": {
	      "type": "text",
	      "fields": {
	        "raw": {"type": "keyword", "ignore_above": 4096}
	      }
	    },
	    "summary": {
	      "type": "text",
	      "fields": {
	        "raw": {"type": "keyword", "ignore_above": 8192}
	      }
	    }
	  }
	}`
	res, err := client.Indices.PutMapping(
		[]string{indexName},
		strings.NewReader(body),
		client.Indices.PutMapping.WithContext(ctx),
	)
	if err != nil {
		zap.L().Debug("Elasticsearch put mapping (cn subfields)", zap.Error(err))
		return
	}
	defer res.Body.Close()
	if res.IsError() {
		b, _ := io.ReadAll(res.Body)
		zap.L().Debug("Elasticsearch put mapping not applied", zap.ByteString("body", b))
	}
}

func UpsertArticle(ctx context.Context, p models.SearchSyncPayload) error {
	if !Enabled() {
		return nil
	}

	body, err := json.Marshal(p)
	if err != nil {
		return err
	}

	docID := fmt.Sprintf("%d", p.ID)
	req := esapi.IndexRequest{
		Index:      indexName,
		DocumentID: docID,
		Body:       bytes.NewReader(body),
	}

	res, err := req.Do(ctx, client)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("elasticsearch index error: %s", string(b))
	}
	return nil
}

func escapeWildcardQuery(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `*`, `\*`)
	s = strings.ReplaceAll(s, `?`, `\?`)
	return s
}

func SearchArticles(ctx context.Context, query string, limit int) ([]map[string]any, error) {
	return SearchArticlesPaged(ctx, query, 1, limit)
}

func SearchArticlesPaged(ctx context.Context, query string, page, size int) ([]map[string]any, error) {
	if !Enabled() {
		return nil, errors.New("search is not enabled")
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	from := (page - 1) * size
	trimmed := strings.TrimSpace(query)
	should := []any{
		map[string]any{
			"multi_match": map[string]any{
				"query":  query,
				"fields": []string{"title^3", "author_name", "tags", "summary"},
			},
		},
	}
	if trimmed != "" {
		w := "*" + escapeWildcardQuery(trimmed) + "*"
		should = append(should,
			map[string]any{
				"wildcard": map[string]any{
					"title.raw": map[string]any{
						"value":            w,
						"case_insensitive": true,
					},
				},
			},
			map[string]any{
				"wildcard": map[string]any{
					"summary.raw": map[string]any{
						"value":            w,
						"case_insensitive": true,
					},
				},
			},
		)
	}

	// bool：全文 + title.raw/summary.raw 子串（利于中文）；未重建索引的旧文档可能仅有 multi_match 命中。
	q := map[string]any{
		"from": from,
		"size": size,
		"query": map[string]any{
			"bool": map[string]any{
				"should":               should,
				"minimum_should_match": 1,
			},
		},
	}

	buf, _ := json.Marshal(q)
	res, err := client.Search(
		client.Search.WithContext(ctx),
		client.Search.WithIndex(indexName),
		client.Search.WithBody(bytes.NewReader(buf)),
	)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.IsError() {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("elasticsearch search error: %s", string(b))
	}

	var raw struct {
		Hits struct {
			Hits []struct {
				Source map[string]any `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]map[string]any, 0, len(raw.Hits.Hits))
	for _, h := range raw.Hits.Hits {
		out = append(out, h.Source)
	}
	return out, nil
}
