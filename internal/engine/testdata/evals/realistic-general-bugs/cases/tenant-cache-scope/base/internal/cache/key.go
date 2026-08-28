package cache

func UserProfileKey(tenantID, userID string) string {
	return "profile:" + tenantID + ":" + userID
}
