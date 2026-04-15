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
}

func ensureIndex() {
	if !Enabled() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	existsRes, err := client.Indices.Exists([]string{indexName}, client.Indices.Exists.WithContext(ctx))
	if err == nil {
		_ = existsRes.Body.Close()
		if existsRes.StatusCode == 200 {
			return
		}
	}

	// 最小可用 mapping：文本字段用 text，tags 用 keyword 数组
	mapping := `{
	  "mappings": {
	    "properties": {
	      "id": {"type": "keyword"},
	      "title": {"type": "text"},
	      "author_name": {"type": "text"},
	      "tags": {"type": "keyword"},
	      "summary": {"type": "text"}
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

func SearchArticles(ctx context.Context, query string, limit int) ([]map[string]any, error) {
	if !Enabled() {
		return nil, errors.New("search is not enabled")
	}
	if limit <= 0 {
		limit = 20
	}

	// multi_match：对 title/author_name/tags/summary 做全文检索
	q := map[string]any{
		"size": limit,
		"query": map[string]any{
			"multi_match": map[string]any{
				"query":  query,
				"fields": []string{"title^3", "author_name", "tags", "summary"},
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
