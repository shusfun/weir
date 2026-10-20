package control

import "testing"

func TestPublicPageValidation(t *testing.T) {
	for _, extra := range []map[string]any{
		{"notice_type": "html"}, {"notice_dismissible": "true"}, {"notice_enabled": "yes"}, {"notice_title": string(make([]byte, 201))}, {"theme": "url(https://invalid.example)"}, {"page_ids": []int{1, 1}}, {"page_ids": []int{-1}}, {"page_ids": []any{1.2}}, {"show_share": "true"}, {"subscription_token": "private"}, {"page_ids": make([]int, 25)},
	} {
		data := map[string]any{"slug": "valid-page", "title": "Public title"}
		for k, v := range extra {
			data[k] = v
		}
		if _, err := decodePublicPage(data); err == nil {
			t.Fatalf("invalid public page accepted: %v", extra)
		}
	}
	for _, theme := range []string{"", "violet", "ocean", "forest"} {
		if _, err := decodePublicPage(map[string]any{"slug": "valid-page", "title": "Public title", "theme": theme, "page_ids": []int{1, 2}}); err != nil {
			t.Fatal(err)
		}
	}
}
