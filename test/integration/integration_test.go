package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mzner/ocis-cli/test/integration/internal/harness"
)

type fixture struct {
	config       harness.Config
	runner       harness.Runner
	browser      *harness.OIDCBrowser
	admin        string
	restricted   string
	managedUser  string
	managedGroup string
	root         string
	local        string
	ctx          context.Context
}

type item struct {
	Name      string     `json:"name"`
	Path      string     `json:"path"`
	Type      string     `json:"type"`
	Size      int64      `json:"size"`
	Tags      []string   `json:"tags"`
	Favorite  *bool      `json:"favorite"`
	Checksums []checksum `json:"checksums"`
}

type treeEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Type  string `json:"type"`
	Size  int64  `json:"size"`
	Depth int    `json:"depth"`
}

type filesystemUsage struct {
	LogicalBytes int64 `json:"logicalBytes"`
	Files        int   `json:"files"`
	Directories  int   `json:"directories"`
	Complete     bool  `json:"complete"`
}

type batchSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type checksum struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

type tagMetadata struct {
	Path string   `json:"path"`
	Tags []string `json:"tags"`
}

type propertyMetadata struct {
	Path      string `json:"path"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Value     string `json:"value"`
}

type drive struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	DriveType string `json:"driveType"`
}

type member struct {
	PermissionID string `json:"permissionId"`
	SubjectID    string `json:"subjectId"`
	DisplayName  string `json:"displayName"`
	Role         string `json:"role"`
}

type directoryUser struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	Username       string `json:"onPremisesSamAccountName"`
	AccountEnabled *bool  `json:"accountEnabled"`
}

type directoryGroup struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	GroupTypes  []string `json:"groupTypes"`
}

type adminSpaceDetails struct {
	Space                drive `json:"space"`
	PermissionsAvailable bool  `json:"permissionsAvailable"`
}

type roleAssignment struct {
	AssignmentID string `json:"assignmentId"`
	RoleID       string `json:"roleId"`
	Role         string `json:"role"`
}

type syncPlan struct {
	Direction string       `json:"direction"`
	Applied   bool         `json:"applied"`
	DryRun    bool         `json:"dryRun"`
	Conflicts int          `json:"conflicts"`
	Moves     int          `json:"moves"`
	Copies    int          `json:"copies"`
	Actions   []syncAction `json:"actions"`
}

type syncAction struct {
	Action   string `json:"action"`
	Path     string `json:"path"`
	FromPath string `json:"fromPath"`
	Target   string `json:"target"`
}

type syncRecovery struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type syncStateSummary struct {
	ID         string `json:"id"`
	Profile    string `json:"profile"`
	Direction  string `json:"direction"`
	LocalRoot  string `json:"localRoot"`
	RemoteRoot string `json:"remoteRoot"`
	Status     string `json:"status"`
}

type syncStateExport struct {
	SchemaVersion string `json:"schemaVersion"`
	ID            string `json:"id"`
	State         struct {
		Binding struct {
			Profile string `json:"profile"`
		} `json:"binding"`
	} `json:"state"`
}

type syncStateRemoval struct {
	ID      string `json:"id"`
	Removed bool   `json:"removed"`
	DryRun  bool   `json:"dryRun"`
}

type syncJob struct {
	Name       string `json:"name"`
	Profile    string `json:"profile"`
	AccountID  string `json:"accountId"`
	SpaceID    string `json:"spaceId"`
	Direction  string `json:"direction"`
	LocalRoot  string `json:"localRoot"`
	RemoteRoot string `json:"remoteRoot"`
}

type syncJobRemoval struct {
	Name    string `json:"name"`
	Removed bool   `json:"removed"`
	DryRun  bool   `json:"dryRun"`
}

type share struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	RecipientName string `json:"recipientName"`
	URL           string `json:"url"`
}

type trashItem struct {
	ID           string `json:"id"`
	OriginalPath string `json:"originalPath"`
}

type version struct {
	ID   string `json:"id"`
	Size int64  `json:"size"`
}

type errorData struct {
	Code int    `json:"code"`
	Kind string `json:"kind"`
}

func TestCLICompatibility(t *testing.T) {
	config, err := harness.LoadConfig()
	if err != nil {
		if os.Getenv("OCIS_INTEGRATION") != "1" {
			t.Skip("set OCIS_INTEGRATION=1 to run black-box oCIS tests")
		}
		t.Fatal(err)
	}
	browser, err := harness.NewOIDCBrowser(config.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	current := &fixture{
		config: config,
		runner: harness.Runner{
			Binary: config.Binary, ConfigPath: filepath.Join(
				t.TempDir(), "config.json",
			), StateDir: filepath.Join(t.TempDir(), "sync-state"),
			Timeout: config.CommandTimeout,
		},
		browser: browser, admin: "it-admin-" + suffix,
		restricted: "it-user-" + suffix, root: "/ocis-cli-it-" + suffix,
		managedUser:  "cli-user-" + suffix,
		managedGroup: "CLI Group " + suffix,
		local:        t.TempDir(), ctx: context.Background(),
	}

	if !t.Run("authentication and registration", current.testAuthentication) {
		t.FailNow()
	}
	t.Run("administrative inventory", current.testAdministrativeInventory)
	t.Run("administrative mutations", current.testAdministrativeMutations)
	t.Run("machine output and exit codes", current.testMachineContract)
	t.Run("personal files and recursive transfers", current.testFiles)
	t.Run("synchronization", current.testSync)
	t.Run("file metadata", current.testMetadata)
	t.Run("versions", current.testVersions)
	t.Run("shares", current.testShares)
	t.Run("search", current.testSearch)
	t.Run("trash", current.testTrash)
	t.Run("spaces permissions and membership", current.testSpaces)

	t.Cleanup(func() {
		for _, profile := range []string{current.admin, current.restricted} {
			_ = current.runner.Run(
				current.ctx, nil, "server", "remove", profile,
			)
		}
	})
}

func (current *fixture) testAdministrativeInventory(t *testing.T) {
	users := decodeData[[]directoryUser](
		t, current.json(t, current.admin, "admin", "user", "list"),
	)
	var currentAdmin directoryUser
	for _, user := range users {
		if user.Username == current.config.AdminUsername {
			currentAdmin = user
			break
		}
	}
	if currentAdmin.ID == "" {
		t.Fatalf(
			"administrator %q absent from user inventory: %#v",
			current.config.AdminUsername, users,
		)
	}
	rawUsers := decodeData[[]directoryUser](
		t, current.json(
			t, current.admin, "admin", "user", "list",
			"--search-raw", `"`+current.config.AdminUsername+`"`,
		),
	)
	if !slices.ContainsFunc(rawUsers, func(user directoryUser) bool {
		return user.ID == currentAdmin.ID
	}) {
		t.Fatalf("raw user search = %#v", rawUsers)
	}
	inspectedUser := decodeData[directoryUser](
		t, current.json(
			t, current.admin, "admin", "user", "info",
			current.config.AdminUsername,
		),
	)
	if inspectedUser.ID != currentAdmin.ID {
		t.Fatalf(
			"inspected user ID = %q, want %q",
			inspectedUser.ID, currentAdmin.ID,
		)
	}

	groups := decodeData[[]directoryGroup](
		t, current.json(t, current.admin, "admin", "group", "list"),
	)
	if len(groups) == 0 || groups[0].ID == "" {
		t.Fatalf("group inventory = %#v", groups)
	}
	inspectedGroup := decodeData[directoryGroup](
		t, current.json(
			t, current.admin, "admin", "group", "info", groups[0].ID,
		),
	)
	if inspectedGroup.ID != groups[0].ID {
		t.Fatalf(
			"inspected group ID = %q, want %q",
			inspectedGroup.ID, groups[0].ID,
		)
	}
	_ = decodeData[[]directoryUser](
		t, current.json(
			t, current.admin, "admin", "group", "member", "list",
			groups[0].ID,
		),
	)

	spaces := decodeData[[]drive](
		t, current.json(t, current.admin, "admin", "space", "list"),
	)
	if len(spaces) == 0 || spaces[0].ID == "" {
		t.Fatalf("global Space inventory = %#v", spaces)
	}
	spaceDetails := decodeData[adminSpaceDetails](
		t, current.json(
			t, current.admin, "admin", "space", "info", spaces[0].ID,
		),
	)
	if spaceDetails.Space.ID != spaces[0].ID {
		t.Fatalf(
			"inspected Space ID = %q, want %q",
			spaceDetails.Space.ID, spaces[0].ID,
		)
	}

	restrictedUsers := current.runner.Run(
		current.ctx, nil, "--json", "--profile", current.restricted,
		"admin", "user", "list",
	)
	assertError(t, restrictedUsers, 3, "authentication")
	restrictedMembers := current.runner.Run(
		current.ctx, nil, "--json", "--profile", current.restricted,
		"admin", "group", "member", "list", groups[0].ID,
	)
	assertError(t, restrictedMembers, 3, "authentication")
	restrictedSpaces := decodeData[[]drive](
		t, current.json(
			t, current.restricted, "admin", "space", "list",
		),
	)
	if len(restrictedSpaces) == 0 {
		t.Fatal("normal user should still see its server-visible Spaces")
	}
}

func (current *fixture) testAdministrativeMutations(t *testing.T) {
	const password = "Disposable-Integration-123!"
	created := decodeData[directoryUser](
		t, current.jsonWithEnvironment(
			t, current.admin,
			map[string]string{"OCIS_USER_PASSWORD": password},
			"admin", "user", "create", current.managedUser,
			"--display-name", "CLI Managed User",
			"--email", current.managedUser+"@example.test",
		),
	)
	if created.ID == "" || created.Username != current.managedUser {
		t.Fatalf("created user = %#v", created)
	}
	t.Cleanup(func() {
		_ = current.runner.Run(
			current.ctx, nil, "--profile", current.admin,
			"admin", "user", "delete", created.ID, "--yes",
		)
	})

	current.success(
		t, nil, "--profile", current.admin,
		"admin", "user", "update", created.ID,
		"--display-name", "CLI Managed User Updated",
	)
	foundUsers := decodeData[[]directoryUser](
		t, current.json(
			t, current.admin, "admin", "user", "list",
			"--search", current.managedUser,
		),
	)
	if !slices.ContainsFunc(foundUsers, func(user directoryUser) bool {
		return user.ID == created.ID
	}) {
		t.Fatalf("literal user search = %#v", foundUsers)
	}
	current.success(
		t, nil, "--profile", current.admin,
		"admin", "user", "disable", created.ID, "--yes",
	)
	disabled := decodeData[directoryUser](
		t, current.json(
			t, current.admin, "admin", "user", "info", created.ID,
		),
	)
	if disabled.AccountEnabled == nil || *disabled.AccountEnabled {
		t.Fatalf("disabled user = %#v", disabled)
	}
	current.success(
		t, nil, "--profile", current.admin,
		"admin", "user", "enable", created.ID,
	)

	group := decodeData[directoryGroup](
		t, current.json(
			t, current.admin, "admin", "group", "create",
			current.managedGroup,
		),
	)
	if group.ID == "" {
		t.Fatalf("created group = %#v", group)
	}
	t.Cleanup(func() {
		_ = current.runner.Run(
			current.ctx, nil, "--profile", current.admin,
			"admin", "group", "delete", group.ID, "--yes",
		)
	})
	renamedGroup := current.managedGroup + " Updated"
	current.success(
		t, nil, "--profile", current.admin,
		"admin", "group", "update", group.ID, "--name", renamedGroup,
	)
	foundGroups := decodeData[[]directoryGroup](
		t, current.json(
			t, current.admin, "admin", "group", "list",
			"--search", renamedGroup,
		),
	)
	if !slices.ContainsFunc(foundGroups, func(candidate directoryGroup) bool {
		return candidate.ID == group.ID
	}) {
		t.Fatalf("literal group search = %#v", foundGroups)
	}
	current.success(
		t, nil, "--profile", current.admin,
		"admin", "group", "member", "add", group.ID, created.ID,
	)
	members := decodeData[[]directoryUser](
		t, current.json(
			t, current.admin, "admin", "group", "member", "list", group.ID,
		),
	)
	if !slices.ContainsFunc(members, func(user directoryUser) bool {
		return user.ID == created.ID
	}) {
		t.Fatalf("group members = %#v", members)
	}

	assignments := decodeData[[]roleAssignment](
		t, current.json(
			t, current.admin, "admin", "user", "role", "list", created.ID,
		),
	)
	if len(assignments) == 0 || assignments[0].AssignmentID == "" ||
		assignments[0].RoleID == "" {
		t.Fatalf("initial role assignments = %#v", assignments)
	}
	current.success(
		t, nil, "--profile", current.admin,
		"admin", "user", "role", "revoke", created.ID,
		assignments[0].AssignmentID, "--yes",
	)
	withoutRole := decodeData[[]roleAssignment](
		t, current.json(
			t, current.admin, "admin", "user", "role", "list", created.ID,
		),
	)
	if len(withoutRole) != 0 {
		t.Fatalf("role was not revoked: %#v", withoutRole)
	}
	current.success(
		t, nil, "--profile", current.admin,
		"admin", "user", "role", "grant", created.ID,
		assignments[0].RoleID,
	)
	restoredRole := decodeData[[]roleAssignment](
		t, current.json(
			t, current.admin, "admin", "user", "role", "list", created.ID,
		),
	)
	if len(restoredRole) != 1 ||
		restoredRole[0].RoleID != assignments[0].RoleID {
		t.Fatalf("restored role assignments = %#v", restoredRole)
	}

	restrictedCreate := current.runner.Run(
		current.ctx, nil, "--json", "--profile", current.restricted,
		"admin", "group", "create", "forbidden-group", "--dry-run",
	)
	assertError(t, restrictedCreate, 3, "authentication")
	restrictedDisable := current.runner.Run(
		current.ctx, nil, "--json", "--profile", current.restricted,
		"admin", "user", "disable", created.ID, "--dry-run",
	)
	assertError(t, restrictedDisable, 3, "authentication")
	restrictedSpaceCreate := current.runner.Run(
		current.ctx, nil, "--json", "--profile", current.restricted,
		"admin", "space", "create",
		"Forbidden Space "+filepath.Base(current.root),
	)
	assertError(t, restrictedSpaceCreate, 3, "authentication")

	current.success(
		t, nil, "--profile", current.admin,
		"admin", "group", "member", "remove", group.ID, created.ID, "--yes",
	)
	current.success(
		t, nil, "--profile", current.admin,
		"admin", "group", "delete", group.ID, "--yes",
	)
	current.success(
		t, nil, "--profile", current.admin,
		"admin", "user", "delete", created.ID, "--yes",
	)
}

func (current *fixture) testMetadata(t *testing.T) {
	remote := current.root + "/source.txt"
	current.success(
		t, nil, "--profile", current.admin,
		"tag", "add", remote, "integration", "approved",
	)
	tagged := decodeData[tagMetadata](
		t, current.json(t, current.admin, "tag", "list", remote),
	)
	if !slices.Contains(tagged.Tags, "integration") ||
		!slices.Contains(tagged.Tags, "approved") {
		t.Fatalf("tags = %#v", tagged)
	}

	current.success(
		t, nil, "--profile", current.admin, "favorite", "set", remote,
	)
	metadata := decodeData[item](
		t, current.json(t, current.admin, "stat", remote),
	)
	if metadata.Favorite == nil || !*metadata.Favorite ||
		len(metadata.Checksums) == 0 {
		t.Fatalf("metadata = %#v", metadata)
	}

	const namespace = "https://github.com/mzner/ocis-cli/metadata"
	current.success(
		t, nil, "--profile", current.admin,
		"property", "set", remote, namespace, "integration-status", "ready",
	)
	property := decodeData[propertyMetadata](
		t, current.json(
			t, current.admin,
			"property", "get", remote, namespace, "integration-status",
		),
	)
	if property.Value != "ready" || property.Namespace != namespace ||
		property.Name != "integration-status" {
		t.Fatalf("property = %#v", property)
	}

	current.success(
		t, nil, "--profile", current.admin,
		"property", "remove", remote, namespace, "integration-status",
	)
	current.success(
		t, nil, "--profile", current.admin,
		"favorite", "unset", remote,
	)
	current.success(
		t, nil, "--profile", current.admin,
		"tag", "remove", remote, "integration", "approved",
	)
}

func (current *fixture) testAuthentication(t *testing.T) {
	current.success(t, nil, "server", "add", current.admin,
		current.config.Server, "--insecure")
	setup := current.json(t, current.admin, "auth", "setup", current.admin)
	setupData := decodeData[map[string]any](t, setup)
	if dynamic, _ := setupData["dynamic"].(bool); !dynamic {
		t.Fatal("server did not advertise dynamic OIDC client registration")
	}
	if _, err := current.runner.OIDCLogin(
		current.ctx, current.browser, current.admin,
		current.config.AdminUsername, current.config.AdminPassword,
	); err != nil {
		t.Fatal(err)
	}
	status := decodeData[map[string]any](
		t, current.json(t, current.admin, "status", current.admin),
	)
	if authenticated, _ := status["authenticated"].(bool); !authenticated {
		t.Fatalf("OIDC status = %#v", status)
	}
	if status["authType"] != "oidc" {
		t.Fatalf("OIDC authType = %#v", status["authType"])
	}
	current.success(t, nil, "--profile", current.admin, "ls", "/")

	if err := harness.ExpireProfile(
		current.runner.ConfigPath, current.admin,
	); err != nil {
		t.Fatal(err)
	}
	current.success(t, nil, "--profile", current.admin, "ls", "/")
	expiry, err := harness.ProfileExpiry(
		current.runner.ConfigPath, current.admin,
	)
	if err != nil {
		t.Fatal(err)
	}
	if expiry <= time.Now().Unix() {
		t.Fatalf("refresh did not update token expiry: %d", expiry)
	}

	current.success(t, map[string]string{
		"OCIS_PASSWORD": current.config.RestrictedPassword,
	}, "login", "--server", current.config.Server,
		"--name", current.restricted, "--auth", "basic",
		"--username", current.config.RestrictedUsername, "--insecure")
	restrictedStatus := decodeData[map[string]any](
		t, current.json(t, current.restricted, "status", current.restricted),
	)
	if restrictedStatus["authType"] != "basic" {
		t.Fatalf("Basic authType = %#v", restrictedStatus["authType"])
	}

	data, err := os.ReadFile(current.runner.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, secretField := range []string{
		`"password"`, `"clientSecret"`, `"accessToken"`, `"refreshToken"`,
	} {
		if strings.Contains(string(data), secretField) {
			t.Fatalf("config contains secret field %s", secretField)
		}
	}
}

func (current *fixture) testMachineContract(t *testing.T) {
	result := current.runner.Run(
		current.ctx, nil, "--json", "--profile", current.admin, "stat",
	)
	assertError(t, result, 2, "usage")

	result = current.runner.Run(
		current.ctx, nil, "--json", "--profile", current.admin,
		"stat", "/definitely-not-present-"+filepath.Base(current.root),
	)
	assertError(t, result, 4, "not_found")

	result = current.runner.Run(
		current.ctx, nil, "--json", "--profile", current.restricted,
		"space", "create", "forbidden-"+filepath.Base(current.root),
	)
	assertError(t, result, 3, "authentication")
}

func (current *fixture) testFiles(t *testing.T) {
	current.success(t, nil, "--profile", current.admin, "mkdir", current.root)
	parents := current.root + "/Projects/2026/Reports"
	current.success(
		t, nil, "--profile", current.admin, "mkdir", "--parents", parents,
	)
	current.success(
		t, nil, "--profile", current.admin, "mkdir", "-p", parents,
	)
	current.success(t, nil, "--profile", current.admin, "stat", parents)
	empty := current.root + "/empty.txt"
	current.success(t, nil, "--profile", current.admin, "touch", empty)
	emptyStat := decodeData[item](
		t, current.json(t, current.admin, "stat", empty),
	)
	if emptyStat.Type != "file" || emptyStat.Size != 0 {
		t.Fatalf("touched file stat = %#v", emptyStat)
	}
	unchanged := current.runSuccess(
		t, nil, "--profile", current.admin, "touch", empty,
	)
	if unchanged.Stdout != "Unchanged "+empty+"\n" {
		t.Fatalf("second touch output = %q", unchanged.Stdout)
	}
	initial := []byte("integration version one\n")
	source := filepath.Join(current.local, "source.txt")
	writeFile(t, source, initial)
	remote := current.root + "/source.txt"
	current.success(
		t, nil, "--profile", current.admin, "upload", source, remote,
	)
	cat := current.runSuccess(
		t, nil, "--profile", current.admin, "cat", remote,
	)
	if cat.Stdout != string(initial) {
		t.Fatalf("cat stdout = %q, want %q", cat.Stdout, initial)
	}

	stat := decodeData[item](
		t, current.json(t, current.admin, "stat", remote),
	)
	if stat.Type != "file" || stat.Size != int64(len(initial)) {
		t.Fatalf("stat = %#v", stat)
	}
	listed := decodeData[[]item](
		t, current.json(t, current.admin, "ls", current.root),
	)
	if !hasItem(listed, "source.txt") {
		t.Fatalf("list does not contain source.txt: %#v", listed)
	}
	jsonl := current.runSuccess(
		t, nil, "--jsonl", "--profile", current.admin, "ls", current.root,
	)
	records, err := harness.DecodeJSONL([]byte(jsonl.Stdout))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != len(listed) {
		t.Fatalf("JSONL records = %d, JSON items = %d", len(records), len(listed))
	}

	downloaded := filepath.Join(current.local, "downloaded.txt")
	current.success(
		t, nil, "--profile", current.admin, "download", remote, downloaded,
	)
	assertFile(t, downloaded, initial)

	copyDirectory := current.root + "/copies"
	moveDirectory := current.root + "/moves"
	current.success(t, nil, "--profile", current.admin, "mkdir", copyDirectory)
	current.success(t, nil, "--profile", current.admin, "mkdir", moveDirectory)
	current.success(t, nil, "--profile", current.admin, "cp", remote, copyDirectory)
	copied := copyDirectory + "/source.txt"
	current.success(t, nil, "--profile", current.admin, "stat", copied)
	current.success(t, nil, "--profile", current.admin, "mv", copied, moveDirectory+"/")
	moved := moveDirectory + "/source.txt"
	current.success(t, nil, "--profile", current.admin, "stat", moved)

	treeSource := filepath.Join(current.local, "tree")
	if err := os.MkdirAll(filepath.Join(treeSource, "nested"), 0750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(treeSource, "top.txt"), []byte("top\n"))
	writeFile(
		t, filepath.Join(treeSource, "nested", "deep.txt"), []byte("deep\n"),
	)
	remoteTree := current.root + "/tree"
	current.success(
		t, nil, "--profile", current.admin, "upload",
		treeSource, remoteTree, "--recursive",
	)
	remoteTreeEntries := decodeData[[]treeEntry](
		t, current.json(
			t, current.admin, "tree", remoteTree,
			"--max-depth", "2", "--max-entries", "10",
		),
	)
	if !hasTreeEntry(remoteTreeEntries, remoteTree+"/nested/deep.txt", 2) ||
		!hasTreeEntry(remoteTreeEntries, remoteTree+"/top.txt", 1) {
		t.Fatalf("tree entries = %#v", remoteTreeEntries)
	}
	usage := decodeData[filesystemUsage](
		t, current.json(
			t, current.admin, "du", remoteTree,
			"--max-depth", "10", "--max-entries", "10",
		),
	)
	if usage.LogicalBytes != 9 || usage.Files != 2 ||
		usage.Directories != 2 || !usage.Complete {
		t.Fatalf("du = %#v", usage)
	}

	batchDirectory := current.root + "/batch"
	batchDownload := filepath.Join(current.local, "batch-download.txt")
	batchManifest := filepath.Join(current.local, "batch.jsonl")
	writeFile(t, batchManifest, []byte(strings.Join([]string{
		fmt.Sprintf(`{"operation":"mkdir","path":%q}`, batchDirectory),
		fmt.Sprintf(
			`{"operation":"touch","path":%q}`, batchDirectory+"/empty.txt",
		),
		fmt.Sprintf(
			`{"operation":"upload","source":%q,"destination":%q}`,
			source, batchDirectory+"/upload.txt",
		),
		fmt.Sprintf(
			`{"operation":"copy","source":%q,"destination":%q}`,
			batchDirectory+"/upload.txt", batchDirectory+"/copy.txt",
		),
		fmt.Sprintf(
			`{"operation":"move","source":%q,"destination":%q}`,
			batchDirectory+"/copy.txt", batchDirectory+"/moved.txt",
		),
		fmt.Sprintf(
			`{"operation":"download","source":%q,"destination":%q}`,
			batchDirectory+"/moved.txt", batchDownload,
		),
		fmt.Sprintf(
			`{"operation":"remove","path":%q}`, batchDirectory+"/moved.txt",
		),
		fmt.Sprintf(
			`{"operation":"remove","path":%q,"recursive":true}`,
			batchDirectory,
		),
	}, "\n")+"\n"))
	batch := decodeData[batchSummary](
		t, current.json(
			t, current.admin, "batch", batchManifest, "--yes",
		),
	)
	if batch.Total != 8 || batch.Succeeded != 8 || batch.Failed != 0 {
		t.Fatalf("batch = %#v", batch)
	}
	assertFile(t, batchDownload, initial)
	treeDestination := filepath.Join(current.local, "tree-download")
	current.success(
		t, nil, "--profile", current.admin, "download",
		remoteTree, treeDestination, "--recursive",
	)
	assertFile(
		t, filepath.Join(treeDestination, "nested", "deep.txt"), []byte("deep\n"),
	)

	bidirectionalConflict := current.runner.Run(
		current.ctx, nil, "--json", "--profile", current.admin,
		"upload", source, remote, "--no-clobber",
	)
	assertError(t, bidirectionalConflict, 5, "conflict")
}

func (current *fixture) testSync(t *testing.T) {
	local := filepath.Join(current.local, "sync-source")
	if err := os.MkdirAll(filepath.Join(local, "nested"), 0750); err != nil {
		t.Fatal(err)
	}
	sourceFile := filepath.Join(local, "nested", "report.txt")
	writeFile(t, sourceFile, []byte("sync baseline\n"))
	remote := current.root + "/sync"

	initial := decodeData[syncPlan](
		t, current.json(
			t, current.admin, "sync", "push", local, remote,
		),
	)
	if !initial.Applied || initial.Conflicts != 0 {
		t.Fatalf("initial sync = %#v", initial)
	}
	downloaded := filepath.Join(current.local, "sync-initial.txt")
	current.success(
		t, nil, "--profile", current.admin, "download",
		remote+"/nested/report.txt", downloaded,
	)
	assertFile(t, downloaded, []byte("sync baseline\n"))

	writeFile(t, sourceFile, []byte("local sync change\n"))
	remoteChange := filepath.Join(current.local, "remote-sync-change.txt")
	writeFile(t, remoteChange, []byte("remote sync change\n"))
	current.success(
		t, nil, "--profile", current.admin, "upload",
		remoteChange, remote+"/nested/report.txt", "--overwrite",
	)
	conflict := current.runner.Run(
		current.ctx, nil, "--json", "--profile", current.admin,
		"sync", "push", local, remote,
	)
	assertError(t, conflict, 5, "conflict")

	planned := decodeData[syncPlan](
		t, current.json(
			t, current.admin, "sync", "push", local, remote, "--dry-run",
		),
	)
	if !planned.DryRun || planned.Conflicts != 1 ||
		!slices.ContainsFunc(planned.Actions, func(action syncAction) bool {
			return action.Action == "conflict" &&
				action.Path == "nested/report.txt"
		}) {
		t.Fatalf("conflict plan = %#v", planned)
	}
	current.success(
		t, nil, "--profile", current.admin,
		"sync", "push", local, remote, "--overwrite",
	)

	pulled := filepath.Join(current.local, "sync-pulled")
	current.success(
		t, nil, "--profile", current.admin,
		"sync", "pull", remote, pulled,
	)
	assertFile(
		t, filepath.Join(pulled, "nested", "report.txt"),
		[]byte("local sync change\n"),
	)

	bidirectionalLocal := filepath.Join(current.local, "sync-bidirectional")
	bidirectionalRemote := current.root + "/sync-bidirectional"
	if err := os.MkdirAll(bidirectionalLocal, 0750); err != nil {
		t.Fatal(err)
	}
	current.success(
		t, nil, "--profile", current.admin,
		"mkdir", bidirectionalRemote,
	)
	current.success(
		t, nil, "--profile", current.admin, "upload",
		remoteChange, bidirectionalRemote+"/remote-only.txt",
	)
	writeFile(
		t, filepath.Join(bidirectionalLocal, "local-only.txt"),
		[]byte("local only\n"),
	)
	bidirectional := decodeData[syncPlan](
		t, current.json(
			t, current.admin, "sync", "bi",
			bidirectionalLocal, bidirectionalRemote, "--dry-run",
		),
	)
	if bidirectional.Direction != "bidirectional" ||
		!bidirectional.DryRun || bidirectional.Applied ||
		!slices.ContainsFunc(
			bidirectional.Actions, func(action syncAction) bool {
				return action.Action == "transfer" &&
					action.Path == "local-only.txt" &&
					action.Target == "remote"
			},
		) || !slices.ContainsFunc(
		bidirectional.Actions, func(action syncAction) bool {
			return action.Action == "transfer" &&
				action.Path == "remote-only.txt" &&
				action.Target == "local"
		},
	) {
		t.Fatalf("bidirectional plan = %#v", bidirectional)
	}
	if _, err := os.Stat(
		filepath.Join(bidirectionalLocal, "remote-only.txt"),
	); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bidirectional dry-run changed local tree: %v", err)
	}

	states := decodeData[[]syncStateSummary](
		t, current.json(
			t, current.admin, "sync", "state", "list",
		),
	)
	if slices.ContainsFunc(states, func(state syncStateSummary) bool {
		return state.Direction == "bidirectional"
	}) {
		t.Fatalf("bidirectional dry-run persisted state: %#v", states)
	}
	bidirectionalApplied := decodeData[syncPlan](
		t, current.json(
			t, current.admin, "sync", "bi",
			bidirectionalLocal, bidirectionalRemote,
		),
	)
	if !bidirectionalApplied.Applied || bidirectionalApplied.DryRun ||
		bidirectionalApplied.Conflicts != 0 {
		t.Fatalf("bidirectional execution = %#v", bidirectionalApplied)
	}
	assertFile(
		t, filepath.Join(bidirectionalLocal, "remote-only.txt"),
		[]byte("remote sync change\n"),
	)
	uploadedLocalOnly := filepath.Join(
		current.local, "bidirectional-uploaded-local-only.txt",
	)
	current.success(
		t, nil, "--profile", current.admin, "download",
		bidirectionalRemote+"/local-only.txt", uploadedLocalOnly,
	)
	assertFile(t, uploadedLocalOnly, []byte("local only\n"))

	writeFile(
		t, filepath.Join(bidirectionalLocal, "local-only.txt"),
		[]byte("local changed again\n"),
	)
	writeFile(t, remoteChange, []byte("remote changed again\n"))
	current.success(
		t, nil, "--profile", current.admin, "upload",
		remoteChange, bidirectionalRemote+"/remote-only.txt", "--overwrite",
	)
	secondBidirectional := decodeData[syncPlan](
		t, current.json(
			t, current.admin, "sync", "bidirectional",
			bidirectionalLocal, bidirectionalRemote,
		),
	)
	if !secondBidirectional.Applied || secondBidirectional.Conflicts != 0 ||
		!slices.ContainsFunc(
			secondBidirectional.Actions, func(action syncAction) bool {
				return action.Action == "transfer" &&
					action.Path == "local-only.txt" &&
					action.Target == "remote"
			},
		) || !slices.ContainsFunc(
		secondBidirectional.Actions, func(action syncAction) bool {
			return action.Action == "transfer" &&
				action.Path == "remote-only.txt" &&
				action.Target == "local"
		},
	) {
		t.Fatalf("second bidirectional execution = %#v", secondBidirectional)
	}
	assertFile(
		t, filepath.Join(bidirectionalLocal, "remote-only.txt"),
		[]byte("remote changed again\n"),
	)
	uploadedLocalUpdate := filepath.Join(
		current.local, "bidirectional-uploaded-local-update.txt",
	)
	current.success(
		t, nil, "--profile", current.admin, "download",
		bidirectionalRemote+"/local-only.txt", uploadedLocalUpdate,
	)
	assertFile(t, uploadedLocalUpdate, []byte("local changed again\n"))

	renamedLocal := filepath.Join(bidirectionalLocal, "renamed-local.txt")
	if err := os.Rename(
		filepath.Join(bidirectionalLocal, "local-only.txt"), renamedLocal,
	); err != nil {
		t.Fatal(err)
	}
	renamePlan := decodeData[syncPlan](
		t, current.json(
			t, current.admin, "sync", "bi",
			bidirectionalLocal, bidirectionalRemote,
		),
	)
	if !renamePlan.Applied || renamePlan.Moves != 1 ||
		!slices.ContainsFunc(renamePlan.Actions, func(action syncAction) bool {
			return action.Action == "move" &&
				action.FromPath == "local-only.txt" &&
				action.Path == "renamed-local.txt" && action.Target == "remote"
		}) {
		t.Fatalf("bidirectional rename plan = %#v", renamePlan)
	}

	writeFile(t, renamedLocal, []byte("preferred local conflict\n"))
	writeFile(t, remoteChange, []byte("preserved remote conflict\n"))
	current.success(
		t, nil, "--profile", current.admin, "upload",
		remoteChange, bidirectionalRemote+"/renamed-local.txt", "--overwrite",
	)
	bidirectionalConflict := current.runner.Run(
		current.ctx, nil, "--json", "--profile", current.admin,
		"sync", "bi", bidirectionalLocal, bidirectionalRemote,
	)
	assertError(t, bidirectionalConflict, 5, "conflict")
	recoveries := decodeData[[]syncRecovery](
		t, current.json(
			t, current.admin, "sync", "recovery", "list",
		),
	)
	if !slices.ContainsFunc(recoveries, func(recovery syncRecovery) bool {
		return recovery.Status == "conflict"
	}) {
		t.Fatalf("bidirectional conflict recovery missing: %#v", recoveries)
	}
	resolved := decodeData[syncPlan](
		t, current.json(
			t, current.admin, "sync", "bi",
			bidirectionalLocal, bidirectionalRemote,
			"--conflict-strategy", "keep-both", "--prefer", "local",
		),
	)
	if !resolved.Applied || resolved.Conflicts != 0 || resolved.Copies != 1 {
		t.Fatalf("keep-both plan = %#v", resolved)
	}
	var conflictCopy string
	for _, action := range resolved.Actions {
		if action.Action == "copy" && strings.Contains(action.Path, ".conflict-remote-") {
			conflictCopy = action.Path
			break
		}
	}
	if conflictCopy == "" {
		t.Fatalf("keep-both conflict copy missing: %#v", resolved.Actions)
	}
	assertFile(
		t, filepath.Join(bidirectionalLocal, filepath.FromSlash(conflictCopy)),
		[]byte("preserved remote conflict\n"),
	)
	remoteConflictCopy := filepath.Join(current.local, "remote-conflict-copy.txt")
	current.success(
		t, nil, "--profile", current.admin, "download",
		bidirectionalRemote+"/"+conflictCopy, remoteConflictCopy,
	)
	assertFile(t, remoteConflictCopy, []byte("preserved remote conflict\n"))
	remotePreferred := filepath.Join(current.local, "remote-preferred.txt")
	current.success(
		t, nil, "--profile", current.admin, "download",
		bidirectionalRemote+"/renamed-local.txt", remotePreferred,
	)
	assertFile(t, remotePreferred, []byte("preferred local conflict\n"))
	recoveries = decodeData[[]syncRecovery](
		t, current.json(
			t, current.admin, "sync", "recovery", "list",
		),
	)
	if len(recoveries) != 0 {
		t.Fatalf("completed recovery journals remain: %#v", recoveries)
	}

	states = decodeData[[]syncStateSummary](
		t, current.json(
			t, current.admin, "sync", "state", "list",
		),
	)
	if !slices.ContainsFunc(states, func(state syncStateSummary) bool {
		return state.Direction == "bidirectional" &&
			state.LocalRoot == bidirectionalLocal &&
			state.RemoteRoot == bidirectionalRemote && state.Status == "valid"
	}) {
		t.Fatalf("bidirectional state missing: %#v", states)
	}
	var pullState syncStateSummary
	for _, state := range states {
		if state.Direction == "pull" && state.LocalRoot == pulled &&
			state.RemoteRoot == remote {
			pullState = state
			break
		}
	}
	if pullState.ID == "" || pullState.Status != "valid" {
		t.Fatalf("pull sync state = %#v; all states = %#v", pullState, states)
	}
	shown := decodeData[syncStateSummary](
		t, current.json(
			t, current.admin, "sync", "state", "show",
			pullState.ID[:12],
		),
	)
	if shown.ID != pullState.ID || shown.Profile != current.admin {
		t.Fatalf("shown sync state = %#v", shown)
	}
	exportedResult, err := current.runner.Success(
		current.ctx, nil,
		"sync", "state", "export", pullState.ID[:12],
	)
	if err != nil {
		t.Fatal(err)
	}
	var exported syncStateExport
	if err := json.Unmarshal([]byte(exportedResult.Stdout), &exported); err != nil {
		t.Fatal(err)
	}
	if exported.SchemaVersion != "1" || exported.ID != pullState.ID ||
		exported.State.Binding.Profile != current.admin {
		t.Fatalf("exported sync state = %#v", exported)
	}
	dryRemoval := decodeData[syncStateRemoval](
		t, current.json(
			t, current.admin, "sync", "state", "remove",
			pullState.ID[:12], "--dry-run",
		),
	)
	if !dryRemoval.DryRun || dryRemoval.Removed {
		t.Fatalf("dry-run sync state removal = %#v", dryRemoval)
	}
	current.success(
		t, nil, "sync", "state", "remove", pullState.ID[:12], "--yes",
	)
	missing := current.runner.Run(
		current.ctx, nil, "--json", "sync", "state", "show",
		pullState.ID[:12],
	)
	assertError(t, missing, 4, "not_found")

	const jobName = "integration-push"
	addedJob := decodeData[syncJob](
		t, current.json(
			t, current.admin, "sync", "job", "add", jobName,
			"--direction", "push", "--local", local, "--remote", remote,
		),
	)
	if addedJob.Name != jobName || addedJob.Profile != current.admin ||
		addedJob.AccountID == "" || addedJob.Direction != "push" ||
		addedJob.LocalRoot != local || addedJob.RemoteRoot != remote {
		t.Fatalf("added sync job = %#v", addedJob)
	}
	jobs := decodeData[[]syncJob](
		t, current.json(
			t, current.admin, "sync", "job", "list",
		),
	)
	if !slices.ContainsFunc(jobs, func(job syncJob) bool {
		return job.Name == jobName
	}) {
		t.Fatalf("sync jobs = %#v", jobs)
	}
	shownJob := decodeData[syncJob](
		t, current.json(
			t, current.admin, "sync", "job", "show", jobName,
		),
	)
	if shownJob.AccountID != addedJob.AccountID {
		t.Fatalf("shown sync job = %#v", shownJob)
	}
	jobPlan := decodeData[syncPlan](
		t, current.json(
			t, current.admin, "sync", "job", "run", jobName, "--dry-run",
		),
	)
	if !jobPlan.DryRun || jobPlan.Conflicts != 0 {
		t.Fatalf("sync job dry-run = %#v", jobPlan)
	}
	current.success(
		t, nil, "--profile", current.admin,
		"sync", "job", "run", jobName,
	)
	wrongProfile := current.runner.Run(
		current.ctx, nil, "--json", "--profile", current.restricted,
		"sync", "job", "run", jobName,
	)
	assertError(t, wrongProfile, 2, "usage")
	dryJobRemoval := decodeData[syncJobRemoval](
		t, current.json(
			t, current.admin, "sync", "job", "remove",
			jobName, "--dry-run",
		),
	)
	if !dryJobRemoval.DryRun || dryJobRemoval.Removed {
		t.Fatalf("dry-run sync job removal = %#v", dryJobRemoval)
	}
	current.success(
		t, nil, "sync", "job", "remove", jobName, "--yes",
	)
	missingJob := current.runner.Run(
		current.ctx, nil, "--json", "sync", "job", "show", jobName,
	)
	assertError(t, missingJob, 4, "not_found")

	const bidirectionalJobName = "integration-bidirectional"
	addedBidirectionalJob := decodeData[syncJob](
		t, current.json(
			t, current.admin, "sync", "job", "add", bidirectionalJobName,
			"--direction", "bidirectional", "--local", bidirectionalLocal,
			"--remote", bidirectionalRemote,
		),
	)
	if addedBidirectionalJob.Direction != "bidirectional" {
		t.Fatalf("added bidirectional sync job = %#v", addedBidirectionalJob)
	}
	bidirectionalJobPlan := decodeData[syncPlan](
		t, current.json(
			t, current.admin, "sync", "job", "run",
			bidirectionalJobName, "--dry-run",
		),
	)
	if !bidirectionalJobPlan.DryRun || bidirectionalJobPlan.Conflicts != 0 {
		t.Fatalf("bidirectional sync job dry-run = %#v", bidirectionalJobPlan)
	}
	current.success(
		t, nil, "sync", "job", "remove", bidirectionalJobName, "--yes",
	)
}

func (current *fixture) testVersions(t *testing.T) {
	remote := current.root + "/source.txt"
	second := []byte("integration version two\n")
	source := filepath.Join(current.local, "source.txt")
	writeFile(t, source, second)
	current.success(
		t, nil, "--profile", current.admin, "upload",
		source, remote, "--overwrite",
	)

	var versions []version
	eventually(t, 90*time.Second, func() bool {
		result := current.runner.Run(
			current.ctx, nil, "--json", "--profile", current.admin,
			"version", "list", remote,
		)
		if result.ExitCode != 0 {
			return false
		}
		envelope, err := harness.DecodeEnvelope([]byte(result.Stdout))
		if err != nil {
			return false
		}
		versions, err = harness.DecodeData[[]version](envelope)
		return err == nil && len(versions) > 0
	})
	historical := filepath.Join(current.local, "historical.txt")
	current.success(
		t, nil, "--profile", current.admin, "version", "download",
		remote, versions[0].ID, historical,
	)
	assertFile(t, historical, []byte("integration version one\n"))
	current.success(
		t, nil, "--profile", current.admin, "version", "restore",
		remote, versions[0].ID, "--yes",
	)
	restored := filepath.Join(current.local, "restored.txt")
	current.success(
		t, nil, "--profile", current.admin, "download", remote, restored,
	)
	assertFile(t, restored, []byte("integration version one\n"))
}

func (current *fixture) testShares(t *testing.T) {
	remote := current.root + "/source.txt"
	direct := decodeData[share](
		t, current.json(
			t, current.admin, "share", "user", "add", remote,
			current.config.RestrictedUsername, "--role", "viewer",
		),
	)
	if direct.ID == "" {
		t.Fatalf("direct share = %#v", direct)
	}
	received := decodeData[[]share](
		t, current.json(t, current.restricted, "share", "received"),
	)
	if !hasShare(received, direct.ID) {
		t.Fatalf("received shares do not contain %s: %#v", direct.ID, received)
	}
	current.success(
		t, nil, "--profile", current.admin,
		"share", "update", direct.ID, "--role", "editor",
	)

	link := decodeData[share](
		t, current.jsonWithEnvironment(
			t, current.admin, map[string]string{
				"OCIS_SHARE_PASSWORD": "Disposable-Link-Password-123!",
			},
			"share", "link", "create", remote, "--password",
		),
	)
	if link.ID == "" || link.URL == "" {
		t.Fatalf("public link = %#v", link)
	}
	links := decodeData[[]share](
		t, current.json(
			t, current.admin, "share", "link", "list", remote,
		),
	)
	if !hasShare(links, link.ID) {
		t.Fatalf("public links do not contain %s: %#v", link.ID, links)
	}
	current.success(
		t, nil, "--profile", current.admin, "share", "link", "revoke", link.ID,
	)
	current.success(
		t, nil, "--profile", current.admin, "share", "remove", direct.ID, "--yes",
	)
}

func (current *fixture) testSearch(t *testing.T) {
	query := filepath.Base(current.root)
	var found bool
	eventually(t, 2*time.Minute, func() bool {
		result := current.runner.Run(
			current.ctx, nil, "--json", "--profile", current.admin,
			"search", query,
		)
		if result.ExitCode != 0 {
			return false
		}
		envelope, err := harness.DecodeEnvelope([]byte(result.Stdout))
		if err != nil {
			return false
		}
		data, err := harness.DecodeData[struct {
			Items []item `json:"items"`
		}](envelope)
		if err != nil {
			return false
		}
		for _, value := range data.Items {
			if strings.Contains(value.Path, current.root) {
				found = true
				return true
			}
		}
		return false
	})
	if !found {
		t.Fatalf("search did not return %s", current.root)
	}
}

func (current *fixture) testTrash(t *testing.T) {
	remote := current.root + "/moves/source.txt"
	current.success(t, nil, "--profile", current.admin, "rm", remote)
	var deleted trashItem
	eventually(t, 30*time.Second, func() bool {
		result := current.runner.Run(
			current.ctx, nil, "--json", "--profile", current.admin,
			"trash", "list",
		)
		if result.ExitCode != 0 {
			return false
		}
		envelope, err := harness.DecodeEnvelope([]byte(result.Stdout))
		if err != nil {
			return false
		}
		items, err := harness.DecodeData[[]trashItem](envelope)
		if err != nil {
			return false
		}
		for _, value := range items {
			if value.OriginalPath == remote {
				deleted = value
				return true
			}
		}
		return false
	})
	current.success(
		t, nil, "--profile", current.admin, "trash", "restore", deleted.ID,
	)
	current.success(t, nil, "--profile", current.admin, "stat", remote)
	current.success(t, nil, "--profile", current.admin, "rm", remote)

	eventually(t, 30*time.Second, func() bool {
		items := decodeData[[]trashItem](
			t, current.json(t, current.admin, "trash", "list"),
		)
		for _, value := range items {
			if value.OriginalPath == remote {
				deleted = value
				return true
			}
		}
		return false
	})
	current.success(
		t, nil, "--profile", current.admin,
		"trash", "remove", deleted.ID, "--yes",
	)
}

func (current *fixture) testSpaces(t *testing.T) {
	name := "CLI Integration " + filepath.Base(current.root)
	created := decodeData[drive](
		t, current.json(t, current.admin, "admin", "space", "create", name),
	)
	if created.ID == "" || created.DriveType != "project" {
		t.Fatalf("created Space = %#v", created)
	}
	spaces := decodeData[[]drive](
		t, current.json(t, current.admin, "space", "list"),
	)
	if !hasDrive(spaces, created.ID) {
		t.Fatalf("Space list does not contain %s", created.ID)
	}
	current.success(
		t, nil, "--profile", current.admin, "space", "use", created.ID,
	)
	current.success(
		t, nil, "--profile", current.admin, "mkdir", "/admin-folder",
	)
	current.success(t, nil, "--profile", current.admin, "space", "unset")

	added := decodeData[member](
		t, current.json(
			t, current.admin, "admin", "space", "member", "add", created.ID,
			current.config.RestrictedUsername, "--role", "viewer",
		),
	)
	if added.PermissionID == "" {
		t.Fatalf("added Space member = %#v", added)
	}
	current.success(
		t, nil, "--profile", current.restricted, "space", "use", created.ID,
	)
	viewerDenied := current.runner.Run(
		current.ctx, nil, "--json", "--profile", current.restricted,
		"mkdir", "/viewer-cannot-write",
	)
	assertError(t, viewerDenied, 3, "authentication")
	current.success(
		t, nil, "--profile", current.admin, "admin", "space", "member", "update",
		created.ID, added.PermissionID, "--role", "editor",
	)
	current.success(
		t, nil, "--profile", current.restricted, "mkdir", "/member-folder",
	)
	current.success(
		t, nil, "--profile", current.restricted, "space", "unset",
	)
	members := decodeData[[]member](
		t, current.json(
			t, current.admin, "admin", "space", "member", "list", created.ID,
		),
	)
	if !hasMember(members, added.PermissionID) {
		t.Fatalf("Space members do not contain %s: %#v", added.PermissionID, members)
	}
	current.success(
		t, nil, "--profile", current.admin, "admin", "space", "member", "remove",
		created.ID, added.PermissionID, "--yes",
	)
	current.success(
		t, nil, "--profile", current.admin, "admin", "space", "disable",
		created.ID, "--yes",
	)
	current.success(
		t, nil, "--profile", current.admin, "admin", "space", "restore", created.ID,
	)
	current.success(
		t, nil, "--profile", current.admin, "admin", "space", "disable",
		created.ID, "--yes",
	)
	current.success(
		t, nil, "--profile", current.admin, "admin", "space", "delete",
		created.ID, "--permanent", "--yes",
	)

	current.success(
		t, nil, "--profile", current.admin, "rm", current.root, "--recursive",
	)
	current.success(
		t, nil, "--profile", current.admin, "trash", "empty", "--yes",
	)
}

func (current *fixture) json(
	t *testing.T, profile string, args ...string,
) harness.Envelope {
	t.Helper()
	return current.jsonWithEnvironment(t, profile, nil, args...)
}

func (current *fixture) jsonWithEnvironment(
	t *testing.T,
	profile string,
	environment map[string]string,
	args ...string,
) harness.Envelope {
	t.Helper()
	command := append([]string{"--json", "--profile", profile}, args...)
	result := current.runSuccess(t, environment, command...)
	envelope, err := harness.DecodeEnvelope([]byte(result.Stdout))
	if err != nil {
		t.Fatalf("%v\n%s", err, harness.DescribeResult(result))
	}
	return envelope
}

func (current *fixture) success(
	t *testing.T, environment map[string]string, args ...string,
) {
	t.Helper()
	_ = current.runSuccess(t, environment, args...)
}

func (current *fixture) runSuccess(
	t *testing.T, environment map[string]string, args ...string,
) harness.Result {
	t.Helper()
	result, err := current.runner.Success(current.ctx, environment, args...)
	if err != nil {
		t.Fatalf("%v\n%s", err, harness.DescribeResult(result))
	}
	return result
}

func decodeData[T any](t *testing.T, envelope harness.Envelope) T {
	t.Helper()
	value, err := harness.DecodeData[T](envelope)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assertError(
	t *testing.T, result harness.Result, code int, kind string,
) {
	t.Helper()
	if result.ExitCode != code {
		t.Fatalf(
			"exit code = %d, want %d\n%s",
			result.ExitCode, code, harness.DescribeResult(result),
		)
	}
	envelope, err := harness.DecodeEnvelope([]byte(result.Stderr))
	if err != nil {
		t.Fatalf("%v\n%s", err, harness.DescribeResult(result))
	}
	data := decodeData[errorData](t, envelope)
	if data.Code != code || data.Kind != kind {
		t.Fatalf("error = %#v, want code %d kind %s", data, code, kind)
	}
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if condition() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition was not satisfied within %s", timeout)
		}
		time.Sleep(time.Second)
	}
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path string, expected []byte) {
	t.Helper()
	actual, err := os.ReadFile(path) //nolint:gosec // integration-owned temporary path
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("file %s = %q, want %q", path, actual, expected)
	}
}

func hasItem(items []item, name string) bool {
	for _, value := range items {
		if value.Name == name {
			return true
		}
	}
	return false
}

func hasTreeEntry(entries []treeEntry, path string, depth int) bool {
	for _, value := range entries {
		if value.Path == path && value.Depth == depth {
			return true
		}
	}
	return false
}

func hasDrive(drives []drive, id string) bool {
	for _, value := range drives {
		if value.ID == id {
			return true
		}
	}
	return false
}

func hasMember(members []member, id string) bool {
	for _, value := range members {
		if value.PermissionID == id {
			return true
		}
	}
	return false
}

func hasShare(shares []share, id string) bool {
	for _, value := range shares {
		if value.ID == id {
			return true
		}
	}
	return false
}

func TestErrorEnvelopeFixture(t *testing.T) {
	envelope, err := harness.DecodeEnvelope([]byte(
		`{"schemaVersion":"1","type":"error","data":{"code":2,"kind":"usage"}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	data := decodeData[errorData](t, envelope)
	if got := fmt.Sprintf("%d:%s", data.Code, data.Kind); got != "2:usage" {
		t.Fatalf("error fixture = %s", got)
	}
}

func TestIntegrationIsOptIn(t *testing.T) {
	if os.Getenv("OCIS_INTEGRATION") == "1" {
		return
	}
	if _, err := harness.LoadConfig(); err == nil {
		t.Fatal("LoadConfig accepted an integration-disabled environment")
	}
}

func TestConfigRejectsInvalidTimeout(t *testing.T) {
	if os.Getenv("OCIS_INTEGRATION") != "1" {
		t.Skip("only relevant when the integration environment is active")
	}
	t.Setenv("OCIS_INTEGRATION_COMMAND_TIMEOUT", "invalid")
	if _, err := harness.LoadConfig(); err == nil {
		t.Fatal("LoadConfig accepted an invalid timeout")
	}
}

func TestJSONFixtureRemainsValid(t *testing.T) {
	value := errorData{Code: 3, Kind: "authentication"}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"code":3,"kind":"authentication"}` {
		t.Fatalf("JSON = %s", data)
	}
}
