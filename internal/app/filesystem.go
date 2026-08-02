package app

import (
	"context"
	"errors"
	"io"

	"github.com/mzner/ocis-cli/internal/credentials"
	"github.com/mzner/ocis-cli/internal/webdav"
)

var errRemoteIsDirectory = webdav.ErrRemoteIsDirectory

func (client *client) davClient() *webdav.Client {
	if client.dav == nil {
		client.dav = webdav.NewClient(webdav.Config{
			Server:      client.profile.Server,
			Username:    client.profile.Username,
			AccountID:   profileIdentity(client.profile),
			AuthType:    client.profile.AuthType,
			Password:    client.profile.Password,
			AccessToken: client.profile.AccessToken,
			SpaceID:     client.selectedSpaceID(),
			UserAgent:   "ocis-cli/" + Version,
			Retries:     client.retries,
			Logger:      client.logger,
			Uploads: uploadSessionStore{
				profile: client.name,
				store:   client.dependencies.UploadSessions,
			},
		}, client.http)
	}
	return client.dav
}

type uploadSessionStore struct {
	profile string
	store   UploadSessionRepository
}

func (store uploadSessionStore) Load(
	key string,
) (webdav.UploadSession, bool, error) {
	session, err := store.store.Get(store.profile, key)
	if errors.Is(err, credentials.ErrNotFound) {
		return webdav.UploadSession{}, false, nil
	}
	if err != nil {
		return webdav.UploadSession{}, false, err
	}
	return webdav.UploadSession{
		UploadURL:         session.UploadURL,
		SourceFingerprint: session.SourceFingerprint,
		ExpiresAt:         session.ExpiresAt,
	}, true, nil
}

func (store uploadSessionStore) Save(
	key string, session webdav.UploadSession,
) error {
	return store.store.Set(store.profile, key, credentials.UploadSession{
		UploadURL:         session.UploadURL,
		SourceFingerprint: session.SourceFingerprint,
		ExpiresAt:         session.ExpiresAt,
	})
}

func (store uploadSessionStore) Delete(key string) error {
	return store.store.Delete(store.profile, key)
}

func (client *client) selectedSpaceID() string {
	if client.space == nil {
		return ""
	}
	return client.space.ID
}

func (client *client) context() context.Context {
	if client.ctx != nil {
		return client.ctx
	}
	return context.Background()
}

func (client *client) list(remote string) ([]item, error) {
	return client.davClient().List(client.context(), remote)
}

func (client *client) stat(remote string) (item, error) {
	return client.davClient().Stat(client.context(), remote)
}

func (client *client) stream(remote string, destination io.Writer) error {
	return client.davClient().DownloadToWriter(
		client.context(), remote, destination,
	)
}

func (client *client) capabilities() (webdav.Capabilities, error) {
	return client.davClient().Capabilities(client.context())
}

func (client *client) getProperty(
	remote string, property webdav.PropertyName,
) (webdav.PropertyValue, error) {
	return client.davClient().GetProperty(client.context(), remote, property)
}

func (client *client) setProperty(
	remote string, property webdav.PropertyName, value string,
) error {
	return client.davClient().SetProperty(
		client.context(), remote, property, value,
	)
}

func (client *client) removeProperty(
	remote string, property webdav.PropertyName,
) error {
	return client.davClient().RemoveProperty(
		client.context(), remote, property,
	)
}

func (client *client) ensureCollection(remote string) error {
	return client.davClient().Mkdir(client.context(), remote)
}

func (client *client) move(source, destination string, overwrite bool) error {
	return client.davClient().Move(client.context(), source, destination, overwrite)
}

func (client *client) copy(source, destination string, overwrite bool) error {
	return client.davClient().Copy(client.context(), source, destination, overwrite)
}

func (client *client) remove(remote string, recursive bool) error {
	return client.davClient().Remove(client.context(), remote, recursive)
}
