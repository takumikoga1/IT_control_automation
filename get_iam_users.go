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
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/joho/godotenv"
)

func main() {
	// .env ファイルを読み込む
	err := godotenv.Load()
	if err != nil {
		log.Printf("Warning: .env file not found.")
	}

	// 環境変数からプロファイル名のリストを取得
	profilesStr := os.Getenv("AWS_PROFILES")
	if profilesStr == "" {
		log.Fatalf("Error: AWS_PROFILES is not set in .env file.")
	}
	profiles := strings.Split(profilesStr, ",")

	// CSVファイルを準備
	csvFileName := "iam_users_list.csv"
	file, err := os.Create(csvFileName)
	if err != nil {
		log.Fatalf("Failed to create file: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// CSVヘッダーを書き込む
	header := []string{"AccountID", "ProfileName", "UserName", "UserID", "Arn", "CreateDate"}
	if err := writer.Write(header); err != nil {
		log.Fatalf("Failed to write header to CSV: %v", err)
	}

	log.Printf("Starting to fetch IAM users from %d accounts...", len(profiles))

	// 各プロファイルをループ処理
	for _, profile := range profiles {
		profile = strings.TrimSpace(profile)
		if profile == "" {
			continue
		}

		log.Printf("Processing profile: %s", profile)

		// プロファイルを使用してAWS設定をロード
		cfg, err := config.LoadDefaultConfig(context.TODO(),
			config.WithSharedConfigProfile(profile),
		)
		if err != nil {
			log.Printf("ERROR: Failed to load config for profile '%s': %v. Skipping...", profile, err)
			continue
		}

		// アカウントIDを取得
		accountID, err := getAccountID(cfg)
		if err != nil {
			log.Printf("ERROR: Failed to get Account ID for profile '%s': %v. Skipping...", profile, err)
			continue
		}

		// IAMクライアントを作成し、全ユーザーを取得
		iamClient := iam.NewFromConfig(cfg)
		paginator := iam.NewListUsersPaginator(iamClient, &iam.ListUsersInput{})
		for paginator.HasMorePages() {
			output, err := paginator.NextPage(context.TODO())
			if err != nil {
				log.Printf("ERROR: Failed to list users for profile '%s': %v", profile, err)
				break 
			}

			// 取得したユーザー情報をCSVに書き込む
			for _, user := range output.Users {
				row := []string{
					accountID,
					profile,
					aws.ToString(user.UserName),
					aws.ToString(user.UserId),
					aws.ToString(user.Arn),
					user.CreateDate.Format(time.RFC3339),
				}
				if err := writer.Write(row); err != nil {
					log.Printf("WARNING: Failed to write row to CSV: %v", err)
				}
			}
		}
		log.Printf("Finished processing profile: %s", profile)
	}

	log.Printf("✅ Successfully exported IAM user data to %s", csvFileName)
}

// getAccountID は現在のアカウントIDを取得するヘルパー関数
func getAccountID(cfg aws.Config) (string, error) {
	stsClient := sts.NewFromConfig(cfg)
	result, err := stsClient.GetCallerIdentity(context.TODO(), &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("could not get caller identity: %w", err)
	}
	return aws.ToString(result.Account), nil
}