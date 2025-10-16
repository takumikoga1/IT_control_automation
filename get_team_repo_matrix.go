package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/google/go-github/v63/github"
	"golang.org/x/oauth2"
	"github.com/joho/godotenv" // 追加
)

func main() {
	// .envファイルを読み込み
	if err := godotenv.Load(); err != nil {
		log.Printf("警告: .envファイルが見つからないか、読み込めませんでした: %v", err)
	}

	token := os.Getenv("GITHUB_TOKEN")
	ownerName := os.Getenv("GITHUB_OWNER") // .envから読み込み
	outputFile := "github_user_team_matrix.csv"

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

	fmt.Printf("Organization '%s' のユーザーとチームの所属情報を取得中...\n", ownerName)

	// 1. 全メンバーと全チームを取得
	optList := &github.ListOptions{PerPage: 100}
	allUsers := []*github.User{}
	allTeams := []*github.Team{}

	// 全ユーザー
	for {
		members, resp, err := client.Organizations.ListMembers(ctx, ownerName, optList) // ownerNameを使用
		if err != nil {
			log.Fatalf("メンバー一覧の取得に失敗しました: %v", err)
		}
		allUsers = append(allUsers, members...)
		if resp.NextPage == 0 {
			break
		}
		optList.Page = resp.NextPage
	}

	// 全チーム
	optList.Page = 1 
	for {
		teams, resp, err := client.Teams.ListTeams(ctx, ownerName, optList) // ownerNameを使用
		if err != nil {
			log.Fatalf("チーム一覧の取得に失敗しました: %v", err)
		}
		allTeams = append(allTeams, teams...)
		if resp.NextPage == 0 {
			break
		}
		optList.Page = resp.NextPage
	}

	// 2. ユーザーごとの所属チーム情報を収集
	userTeamMap := make(map[string]map[string]bool) 

	for _, user := range allUsers {
		userTeamMap[user.GetLogin()] = make(map[string]bool)
	}

	for _, team := range allTeams {
		fmt.Printf("  チーム: %s のメンバーを取得...\n", team.GetName())
		optList.Page = 1 

		for {
			// ownerNameを使用
			members, resp, err := client.Teams.ListTeamMembersBySlug(ctx, ownerName, team.GetSlug(), &github.TeamListTeamMembersOptions{ListOptions: *optList}) 
			if err != nil {
				log.Printf("チーム %s のメンバー取得に失敗しました: %v", team.GetName(), err)
				break
			}
			for _, member := range members {
				if _, ok := userTeamMap[member.GetLogin()]; ok {
					userTeamMap[member.GetLogin()][team.GetName()] = true
				}
			}

			if resp.NextPage == 0 {
				break
			}
			optList.Page = resp.NextPage
		}
	}

	// 3. CSVに書き出し (以降は変更なし)
	userLogins := []string{}
	for login := range userTeamMap {
		userLogins = append(userLogins, login)
	}
	sort.Strings(userLogins)

	teamNames := []string{}
	for _, team := range allTeams {
		teamNames = append(teamNames, team.GetName())
	}
	sort.Strings(teamNames)

	header := append([]string{"Login (ユーザー名)"}, teamNames...)
	writer.Write(header)

	for _, login := range userLogins {
		row := []string{login}
		teamsBelonging := userTeamMap[login]
		for _, teamName := range teamNames {
			is_member := ""
			if teamsBelonging[teamName] {
				is_member = "Yes"
			}
			row = append(row, is_member)
		}
		writer.Write(row)
	}

	fmt.Printf("\n✅ ユーザー → チームのマトリクスを '%s' に保存しました。\n", outputFile)
}