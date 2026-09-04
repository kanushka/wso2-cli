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

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/iam/internal/thunder"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

const (
	UsersSchema = "iam.users/v1"
	UserSchema  = "iam.user/v1"
)

type userCreateFlags struct {
	email, givenName, familyName, passwordVariable, ou string
}

func userCommands() (*cobra.Command, *cobra.Command, *cobra.Command, *userCreateFlags) {
	family := &cobra.Command{Use: "users", Short: "Manage ThunderID users."}
	list := &cobra.Command{Use: "list", Short: "List the users."}
	flags := &userCreateFlags{}
	create := &cobra.Command{
		Use:   "create <username> --email <address> --password-variable <VAR>",
		Short: "Create a person, with a password read from the environment.",
	}
	create.Flags().StringVar(&flags.email, "email", "", "The user's email address.")
	create.Flags().StringVar(&flags.givenName, "given-name", "", "The user's given name.")
	create.Flags().StringVar(&flags.familyName, "family-name", "", "The user's family name.")
	create.Flags().StringVar(&flags.passwordVariable, "password-variable", "WSO2_IAM_USER_PASSWORD",
		"The environment variable holding the new user's password; never the password itself.")
	create.Flags().StringVar(&flags.ou, "ou", DefaultOU, "The organization unit the user belongs to.")
	family.AddCommand(list, create)
	return family, list, create, flags
}

func usersList(ctx context.Context, request module.Request) (result.Result, error) {
	client, err := managementClient(ctx, request)
	if err != nil {
		return result.Result{}, err
	}
	var listed thunder.UserList
	if err := client.Get(ctx, "/users", &listed); err != nil {
		return result.Result{}, thunder.Problem(err, "the user listing")
	}
	names := make([]string, 0, len(listed.Users))
	for _, user := range listed.Users {
		names = append(names, user.Username())
	}
	return result.New(UsersSchema).
		With("total", "Total", fmt.Sprintf("%d", listed.TotalResults)).
		With("users", "Users", joined(names)).
		With(NextField, "Next", "Run wso2 iam users create <username> --email <address> "+
			"--password-variable WSO2_IAM_USER_PASSWORD to add one."), nil
}

func usersCreate(command *cobra.Command, flags *userCreateFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		username, err := oneArgument(command, "a username")
		if err != nil {
			return result.Result{}, err
		}
		if flags.email == "" {
			return result.Result{}, problem.New(problem.CategoryUsage, "iam.missing_flag",
				"wso2 iam users create needs --email").WithRecovery("Pass --email <address>.")
		}
		password, err := secretFromEnvironment(flags.passwordVariable, "the new user's password")
		if err != nil {
			return result.Result{}, err
		}
		client, err := managementClient(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		var listed thunder.UserList
		if err := client.Get(ctx, "/users", &listed); err != nil {
			return result.Result{}, thunder.Problem(err, "the user listing")
		}
		user, created := thunder.User{}, false
		for _, candidate := range listed.Users {
			if candidate.Username() == username {
				user = candidate
			}
		}
		if user.ID == "" {
			body := thunder.User{OUID: flags.ou, Type: "Person", Attributes: map[string]any{
				"username": username, "email": flags.email, "password": password,
			}}
			if flags.givenName != "" {
				body.Attributes["given_name"] = flags.givenName
			}
			if flags.familyName != "" {
				body.Attributes["family_name"] = flags.familyName
			}
			if err := client.Post(ctx, "/users", body, &user); err != nil {
				return result.Result{}, thunder.Problem(err, "the user creation")
			}
			created = true
		}
		return result.New(UserSchema).
			With("id", "ID", user.ID).
			With("username", "Username", username).
			With("created", "Created", createdWord(created)).
			With(NextField, "Next", fmt.Sprintf("Run wso2 iam roles create <role> --resource-server <name> "+
				"--permission <a:b:c> --assign-user %s to grant this user access.", username)), nil
	}
}
