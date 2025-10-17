package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	// "github.com/aws/aws-sdk-go-v2/service/iam/types" // この行を削除しました
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Printf("Warning: .env file not found.")
	}

	profilesStr := os.Getenv("AWS_PROFILES")
	if profilesStr == "" {
		log.Fatalf("Error: AWS_PROFILES is not set in .env file.")
	}
	profiles := strings.Split(profilesStr, ",")

	csvFileName := "iam_users_list.csv"
	file, err := os.Create(csvFileName)
	if err != nil {
		log.Fatalf("Failed to create file: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	header := []string{"AccountID", "ProfileName", "UserName", "UserID", "Arn", "CreateDate", "Groups"}
	if err := writer.Write(header); err != nil {
		log.Fatalf("Failed to write header to CSV: %v", err)
	}

	log.Printf("Starting to fetch IAM users and groups from %d accounts...", len(profiles))

	for _, profile := range profiles {
		profile = strings.TrimSpace(profile)
		if profile == "" {
			continue
		}

		log.Printf("Processing profile: %s", profile)

		cfg, err := config.LoadDefaultConfig(context.TODO(),
			config.WithSharedConfigProfile(profile),
		)
		if err != nil {
			log.Printf("ERROR: Failed to load config for profile '%s': %v. Skipping...", profile, err)
			continue
		}

		accountID, err := getAccountID(cfg)
		if err != nil {
			log.Printf("ERROR: Failed to get Account ID for profile '%s': %v. Skipping...", profile, err)
			continue
		}

		iamClient := iam.NewFromConfig(cfg)
		userPaginator := iam.NewListUsersPaginator(iamClient, &iam.ListUsersInput{})
		for userPaginator.HasMorePages() {
			userOutput, err := userPaginator.NextPage(context.TODO())
			if err != nil {
				log.Printf("ERROR: Failed to list users for profile '%s': %v", profile, err)
				break
			}

			for _, user := range userOutput.Users {
				groups, err := getGroupsForUser(iamClient, user.UserName)
				if err != nil {
					log.Printf("WARNING: Failed to get groups for user '%s' in profile '%s': %v", *user.UserName, profile, err)
				}
				
				row := []string{
					accountID,
					profile,
					aws.ToString(user.UserName),
					aws.ToString(user.UserId),
					aws.ToString(user.Arn),
					user.CreateDate.Format(time.RFC3339),
					strings.Join(groups, ","),
				}
				if err := writer.Write(row); err != nil {
					log.Printf("WARNING: Failed to write row to CSV: %v", err)
				}
			}
		}
		log.Printf("Finished processing profile: %s", profile)
	}

	log.Printf("✅ Successfully exported IAM user and group data to %s", csvFileName)
}

func getGroupsForUser(client *iam.Client, userName *string) ([]string, error) {
	var groups []string
	groupPaginator := iam.NewListGroupsForUserPaginator(client, &iam.ListGroupsForUserInput{
		UserName: userName,
	})

	for groupPaginator.HasMorePages() {
		output, err := groupPaginator.NextPage(context.TODO())
		if err != nil {
			return nil, err
		}
		for _, group := range output.Groups {
			groups = append(groups, *group.GroupName)
		}
	}
	return groups, nil
}

func getAccountID(cfg aws.Config) (string, error) {
	stsClient := sts.NewFromConfig(cfg)
	result, err := stsClient.GetCallerIdentity(context.TODO(), &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("could not get caller identity: %w", err)
	}
	return aws.ToString(result.Account), nil
}