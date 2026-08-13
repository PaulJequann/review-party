package cache

const profilePrefix = "profile:"

func UserProfileKey(userID string) string {
	return profilePrefix + userID
}
