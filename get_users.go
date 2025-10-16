package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/google/go-github/v63/github"
	"golang.org/x/oauth2"
	"github.com/joho/godotenv"
)

// 過去のユーザーデータ構造体
type OldUserData struct {
	Name  string
	Email string
}

// 過去のCSVからLoginとName, Emailのマップを作成する関数
func loadOldUsers(filename string) map[string]OldUserData {
	oldUsers := make(map[string]OldUserData)
	file, err := os.Open(filename)
	if err != nil {
		log.Printf("警告: 過去のCSVファイル '%s' の読み込みに失敗しました。自動埋め込みはスキップされます。", filename)
		return oldUsers
	}
	defer file.Close()

	reader := csv.NewReader(file)
	// ヘッダー行をスキップ
	if _, err := reader.Read(); err != nil {
		log.Printf("警告: 過去のCSVファイルからヘッダーの読み込みに失敗しました。")
		return oldUsers
	}

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("警告: 過去のCSVファイルのレコード読み込み中にエラーが発生しました: %v", err)
			continue
		}
		
		// 期待される列数があるか確認 (Login=0, Name=1, Email=2 を含むため最低3列)
		if len(record) > 2 {
			login := record[0] 
			name := record[1]  // 氏名 (インデックス 1)
			email := record[2] // メールアドレス (インデックス 2)
			
			if login != "" {
				// 氏名かメールアドレスの少なくとも一方があれば記録
				if name != "" || email != "" {
					oldUsers[login] = OldUserData{Name: name, Email: email}
				}
			}
		}
	}
	log.Printf("過去のCSVファイルから %d 件の氏名/メールアドレス情報を読み込みました。", len(oldUsers))
	return oldUsers
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("警告: .envファイルが見つからないか、読み込めませんでした: %v", err)
	}

	token := os.Getenv("GITHUB_TOKEN")
	ownerName := os.Getenv("GITHUB_OWNER") 
	outputFile := "github_user_list.csv"
	
	const oldCsvFile = "old_user_list.csv" 
	
	// 過去のユーザーデータを読み込み
	oldUserMap := loadOldUsers(oldCsvFile)

	if token == "" || ownerName == "" { 
		log.Fatal("エラー: GITHUB_TOKEN または GITHUB_OWNER が .envファイル、または環境変数で設定されていません。")
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// CSVファイル作成
	file, err := os.Create(outputFile)
	if err != nil {
		log.Fatalf("CSVファイルの作成に失敗しました: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// ヘッダーを書き込み
	header := []string{"Login (ユーザー名)", "ID", "Name (氏名)", "Email", "Type"}
	writer.Write(header)

	fmt.Printf("Organization '%s' のメンバーを取得中...\n", ownerName)

	opt := &github.ListMembersOptions{ListOptions: github.ListOptions{PerPage: 100}}
	allUsers := []*github.User{}

	// ページネーションで全ユーザーを取得
	for {
		members, resp, err := client.Organizations.ListMembers(ctx, ownerName, opt) 
		if err != nil {
			log.Fatalf("メンバー一覧の取得に失敗しました: %v", err)
		}
		allUsers = append(allUsers, members...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	// 各ユーザーの詳細情報を取得し、CSVに書き込む
	for _, member := range allUsers {
		user, _, err := client.Users.Get(ctx, member.GetLogin())
		if err != nil {
			log.Printf("ユーザー %s の詳細情報の取得に失敗しました: %v", member.GetLogin(), err)
			continue
		}
		
		login := user.GetLogin()
		githubName := user.GetName() 
		githubEmail := user.GetEmail() // GitHubから取得したメールアドレス
		
		finalName := githubName 
		finalEmail := githubEmail // デフォルトはGitHubの名前とメールアドレス

		// 🌟 過去データで氏名とメールアドレスを上書き/埋め込み 🌟
		if oldData, ok := oldUserMap[login]; ok {
			// 過去のCSVに氏名が存在する場合、それを採用
			if oldData.Name != "" {
				finalName = oldData.Name
			}
			// 過去のCSVにメールアドレスが存在する場合、それを採用（GitHub側が空欄の場合も含む）
			if oldData.Email != "" {
				finalEmail = oldData.Email
			}
		} 
		
		// GitHub、過去データ共に名前/メールが空の場合は空文字を維持
		if finalName == "" {
			finalName = "" 
		}
		if finalEmail == "" {
			finalEmail = ""
		}
		
		row := []string{
			login,
			fmt.Sprintf("%d", user.GetID()),
			finalName, 
			finalEmail, // 埋め込まれたメールアドレスを使用
			user.GetType(),
		}
		writer.Write(row)
		fmt.Printf("  取得: %s (氏名: %s, Email: %s)\n", login, finalName, finalEmail)
	}

	fmt.Printf("\n✅ ユーザー一覧を '%s' に保存しました。過去データに基づき氏名とメールアドレスが自動埋め込みされました。\n", outputFile)
}