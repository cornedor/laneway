package jira

import (
	"maps"
	"regexp"
	"slices"
)

// stockTypeAvatars are Jira's own issue type icons by avatar number, named
// for what they show. They hold on every site whatever the type is called
// ("Taak" is 10318), and a custom type picking one of them gets its look.
var stockTypeAvatars = map[string]string{
	"10300": "task",
	"10303": "bug",
	"10304": "incident",
	"10306": "document",
	"10307": "epic",
	"10308": "problem",
	"10309": "design",
	"10310": "improvement",
	"10311": "add",
	"10312": "remove",
	"10313": "checklist",
	"10314": "money",
	"10315": "story",
	"10316": "subtask",
	"10318": "task",
	"10320": "question",
	"10321": "code",
	"10322": "idea",
	"10323": "security",
	"10757": "mail",
	"10758": "folder",
	"10759": "work",
	"10760": "event",
	"10761": "money",
	"10762": "devices",
	"10763": "checklist",
	"10764": "building",
	"10765": "add-user",
	"10766": "user-check",
	"10767": "user",
	"10768": "add",
	"10769": "monitor",
}

// typeAvatarNum finds the avatar number in an issue type's iconUrl: Cloud's
// universal_avatar path, or Data Center's viewavatar query.
var typeAvatarNum = regexp.MustCompile(`/universal_avatar/view/type/issuetype/avatar/(\d+)|/secure/viewavatar\?(?:.*&)?avatarId=(\d+)`)

// TypeKind is the stock icon an issue type's icon URL shows ("bug",
// "story", "task", "epic", "subtask", "question", …; see
// stockTypeAvatars), "" for any other icon.
func TypeKind(iconURL string) string {
	m := typeAvatarNum.FindStringSubmatch(iconURL)
	if m == nil {
		return ""
	}
	return stockTypeAvatars[m[1]+m[2]]
}

// TypeKinds lists every kind TypeKind returns, for tables keyed by them.
func TypeKinds() []string {
	return slices.Compact(slices.Sorted(maps.Values(stockTypeAvatars)))
}
