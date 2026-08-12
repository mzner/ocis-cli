package eventstream

// TypeInfo describes an event name currently known by the CLI.
type TypeInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// KnownTypes returns the event names currently emitted by oCIS services and
// consumed by the oCIS Web client. A server can add further event names without
// requiring a CLI update; event watch does not reject unknown names.
func KnownTypes() []TypeInfo {
	return []TypeInfo{
		{Name: "userlog-notification", Description: "Unread notification received"},
		{Name: "postprocessing-finished", Description: "Upload processing finished"},
		{Name: "file-locked", Description: "File locked"},
		{Name: "file-unlocked", Description: "File unlocked"},
		{Name: "file-touched", Description: "File changed"},
		{Name: "item-renamed", Description: "Resource renamed"},
		{Name: "item-trashed", Description: "Resource moved to trash"},
		{Name: "item-restored", Description: "Resource restored from trash"},
		{Name: "item-moved", Description: "Resource moved"},
		{Name: "folder-created", Description: "Folder created"},
		{Name: "space-member-added", Description: "Space member added"},
		{Name: "space-member-removed", Description: "Space member removed"},
		{Name: "space-share-updated", Description: "Space membership updated"},
		{Name: "share-created", Description: "Share created"},
		{Name: "share-removed", Description: "Share removed"},
		{Name: "share-updated", Description: "Share updated"},
		{Name: "link-created", Description: "Public link created"},
		{Name: "link-removed", Description: "Public link removed"},
		{Name: "link-updated", Description: "Public link updated"},
		{Name: "backchannel-logout", Description: "Login session ended by server"},
	}
}

// Description returns the friendly description of name, or name itself when
// the server emitted a newer event unknown to this CLI.
func Description(name string) string {
	for _, eventType := range KnownTypes() {
		if eventType.Name == name {
			return eventType.Description
		}
	}
	return name
}
