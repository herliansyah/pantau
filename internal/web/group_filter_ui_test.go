package web

import (
	"strings"
	"testing"
)

func TestGroupFilterNoGroupingOption(t *testing.T) {
	html := string(embeddedHTML)

	// 1. Dropdown markup must include the 'none' / No Grouping option
	if !strings.Contains(html, `<option value="none" data-i18n="filter_no_grouping">No Grouping</option>`) {
		t.Errorf("expected index.html to contain 'none' option with filter_no_grouping data-i18n attribute")
	}

	// 2. Translations in both en and id
	if !strings.Contains(html, `"filter_no_grouping": "No Grouping"`) {
		t.Errorf("expected EN translation for filter_no_grouping")
	}
	if !strings.Contains(html, `"filter_no_grouping": "Tanpa Grouping"`) {
		t.Errorf("expected ID translation for filter_no_grouping")
	}

	// 3. updateGroupSuggestions must include filter_no_grouping and preserve 'none' selection
	if !strings.Contains(html, "data-i18n=\"filter_no_grouping\"") {
		t.Errorf("expected updateGroupSuggestions to append 'none' option with filter_no_grouping")
	}
	if !strings.Contains(html, `currentVal === 'none'`) {
		t.Errorf("expected updateGroupSuggestions to preserve currentVal when 'none'")
	}

	// 4. renderGridView and renderListView must handle 'none' to display hosts without group dividers
	if !strings.Contains(html, `const isNoGrouping = groupVal === 'none';`) {
		t.Errorf("expected isNoGrouping check in renderGridView / renderListView")
	}
}
