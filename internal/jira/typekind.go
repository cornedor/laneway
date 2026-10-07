package jira

import "regexp"

// stockTypeAvatars are the avatar numbers of Jira's own issue type icons.
// They hold on every site whatever the type is called ("Taak" is 10318).
var stockTypeAvatars = map[string]string{
	"10303": "bug",
	"10307": "epic",
	"10315": "story",
	"10316": "subtask",
	"10318": "task",
}

// typeAvatarNum finds the avatar number in an issue type's iconUrl: Cloud's
// universal_avatar path, or Data Center's viewavatar query.
var typeAvatarNum = regexp.MustCompile(`/universal_avatar/view/type/issuetype/avatar/(\d+)|/secure/viewavatar\?(?:.*&)?avatarId=(\d+)`)

// TypeKind is the stock issue type an icon URL shows: "bug", "story",
// "task", "epic" or "subtask"; "" for any other icon.
func TypeKind(iconURL string) string {
	m := typeAvatarNum.FindStringSubmatch(iconURL)
	if m == nil {
		return ""
	}
	return stockTypeAvatars[m[1]+m[2]]
}
