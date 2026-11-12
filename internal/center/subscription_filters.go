package center

import (
	"github.com/gin-gonic/gin"
	"github.com/shusfun/weir/internal/sub"
)

func checkSubscriptionFilters(c *gin.Context, include, exclude *string) bool {
	for name, text := range map[string]*string{"filter_include": include, "filter_exclude": exclude} {
		if text != nil {
			if err := sub.ValidateFilterRules(*text); err != nil {
				failDetail(c, 400, "invalid_"+name, err.Error(), nil)
				return false
			}
		}
	}
	return true
}
