// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package thunder

// The shapes below are the parts of ThunderID 1.0.0's management API this
// module reads and writes. Unknown members are ignored on the way in and
// never sent on the way out.

// ResourceServer is a protected API ThunderID issues tokens for.
type ResourceServer struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Identifier  string `json:"identifier"`
	OUID        string `json:"ouId,omitempty"`
}

// ResourceServerList is the answer to GET /resource-servers.
type ResourceServerList struct {
	TotalResults    int              `json:"totalResults"`
	ResourceServers []ResourceServer `json:"resourceServers"`
}

// Resource is one node of a resource server's permission tree.
type Resource struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name"`
	Handle     string `json:"handle"`
	Parent     string `json:"parent,omitempty"`
	Permission string `json:"permission,omitempty"`
}

// ResourceList is the answer to GET /resource-servers/{id}/resources.
type ResourceList struct {
	TotalResults int        `json:"totalResults"`
	Resources    []Resource `json:"resources"`
}

// OAuthConfig is the OAuth 2.0 inbound configuration of an application.
type OAuthConfig struct {
	ClientID                string   `json:"clientId"`
	ClientSecret            string   `json:"clientSecret,omitempty"`
	RedirectURIs            []string `json:"redirectUris,omitempty"`
	GrantTypes              []string `json:"grantTypes"`
	ResponseTypes           []string `json:"responseTypes,omitempty"`
	TokenEndpointAuthMethod string   `json:"tokenEndpointAuthMethod"`
	PKCERequired            bool     `json:"pkceRequired"`
	PublicClient            bool     `json:"publicClient"`
}

// InboundAuth is one inbound authentication arrangement of an application.
type InboundAuth struct {
	Type   string      `json:"type"`
	Config OAuthConfig `json:"config"`
}

// Application is a ThunderID application.
type Application struct {
	ID               string        `json:"id,omitempty"`
	OUID             string        `json:"ouId,omitempty"`
	Name             string        `json:"name"`
	Type             string        `json:"type,omitempty"`
	Description      string        `json:"description,omitempty"`
	AuthFlowID       string        `json:"authFlowId,omitempty"`
	URL              string        `json:"url,omitempty"`
	AllowedUserTypes []string      `json:"allowedUserTypes,omitempty"`
	ClientID         string        `json:"clientId,omitempty"`
	InboundAuth      []InboundAuth `json:"inboundAuthConfig,omitempty"`
}

// OAuthClientID is the client id the application's OAuth configuration
// carries, whichever member the deployment reported it in.
func (a Application) OAuthClientID() string {
	for _, inbound := range a.InboundAuth {
		if inbound.Type == "oauth2" && inbound.Config.ClientID != "" {
			return inbound.Config.ClientID
		}
	}
	return a.ClientID
}

// ApplicationList is the answer to GET /applications.
type ApplicationList struct {
	TotalResults int           `json:"totalResults"`
	Applications []Application `json:"applications"`
}

// User is a ThunderID user; its attributes are schema-defined.
type User struct {
	ID         string         `json:"id,omitempty"`
	OUID       string         `json:"ouId,omitempty"`
	Type       string         `json:"type"`
	Attributes map[string]any `json:"attributes"`
}

// Username reports the username attribute, or the id when there is none.
func (u User) Username() string {
	if name, ok := u.Attributes["username"].(string); ok && name != "" {
		return name
	}
	return u.ID
}

// UserList is the answer to GET /users.
type UserList struct {
	TotalResults int    `json:"totalResults"`
	Users        []User `json:"users"`
}

// RolePermission grants permissions on one resource server.
type RolePermission struct {
	ResourceServerID string   `json:"resourceServerId"`
	Permissions      []string `json:"permissions"`
}

// Assignment is one assignee of a role: a user or an app.
type Assignment struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Role is a ThunderID role.
type Role struct {
	ID          string           `json:"id,omitempty"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	OUID        string           `json:"ouId,omitempty"`
	Permissions []RolePermission `json:"permissions,omitempty"`
	Assignments []Assignment     `json:"assignments,omitempty"`
}

// RoleList is the answer to GET /roles.
type RoleList struct {
	TotalResults int    `json:"totalResults"`
	Roles        []Role `json:"roles"`
}

// AssignmentChange is the body of POST /roles/{id}/assignments/add.
type AssignmentChange struct {
	Assignments []Assignment `json:"assignments"`
}
