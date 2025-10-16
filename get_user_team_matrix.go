package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"sort"
	"sync" // 並行処理のためのパッケージ

	"github.com/google/go-github/v63/github"
	"golang.org/x/oauth2"
	"github.com/joho/godotenv"
)

func main() {
	// .envファイルを読み込み
	if err := godotenv.Load(); err != nil {
		log.Printf("警告: .envファイルが見つからないか、読み込めませんでした: %v", err)
	}

	token := os.Getenv("GITHUB_TOKEN")
	ownerName := os.Getenv("GITHUB_OWNER")
	outputFile := "github_user_team_concurrent_matrix.csv"

	if token == "" || ownerName == "" {
		log.Fatal("エラー: GITHUB_TOKEN または GITHUB_OWNER が .envファイル、または環境変数で設定されていません。")
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	fmt.Printf("Organization '%s' のユーザーとチームの所属情報を並行取得中...\n", ownerName)

	optList := github.ListOptions{PerPage: 100}
	
	// ----------------------------------------------------
	// 1. 全メンバーと全チームを取得 (同期処理)
	// ----------------------------------------------------
	
	// 全メンバー（ユーザー）の取得
	optMembers := &github.ListMembersOptions{ListOptions: optList}
	allUsers := []*github.User{}
	for {
		members, resp, err := client.Organizations.ListMembers(ctx, ownerName, optMembers)
		if err != nil {
			log.Fatalf("メンバー一覧の取得に失敗しました: %v", err)
		}
		allUsers = append(allUsers, members...)
		if resp.NextPage == 0 {
			break
		}
		optMembers.Page = resp.NextPage
	}

	// 全チームの取得
	optTeam := &github.ListOptions{PerPage: 100}
	allTeams := []*github.Team{}
	for {
		teams, resp, err := client.Teams.ListTeams(ctx, ownerName, optTeam)
		if err != nil {
			log.Fatalf("チーム一覧の取得に失敗しました: %v", err)
		}
		allTeams = append(allTeams, teams...)
		if resp.NextPage == 0 {
			break
		}
		optTeam.Page = resp.NextPage
	}

	// ----------------------------------------------------
	// 2. ユーザーごとの所属チーム情報を並行して収集
	// ----------------------------------------------------
	
	// userTeamMap: userLogin -> teamName -> true
	userTeamMap := make(map[string]map[string]bool) 
	var wg sync.WaitGroup
	var mapLock sync.Mutex // マップ書き込み用のロック

	// userTeamMapを初期化
	for _, user := range allUsers {
		userTeamMap[user.GetLogin()] = make(map[string]bool)
	}

	fmt.Printf("-> チーム所属メンバーの並行処理を開始 (チーム数: %d)\n", len(allTeams))

	for _, team := range allTeams {
		wg.Add(1)
		// 各チームのメンバー取得をゴルーチンで実行
		go func(t *github.Team) {
			defer wg.Done()
			
			teamName := t.GetName()
			optTeamMember := &github.TeamListTeamMembersOptions{ListOptions: github.ListOptions{PerPage: 100}}
			
			// チームメンバーを取得
			for {
				members, resp, err := client.Teams.ListTeamMembersBySlug(ctx, ownerName, t.GetSlug(), optTeamMember)
				if err != nil {
					log.Printf("警告: チーム %s のメンバー取得に失敗: %v", teamName, err)
					return // このチームの処理を終了
				}
				
				mapLock.Lock() // ロック
				for _, member := range members {
					login := member.GetLogin()
					// userTeamMapに存在するかチェックし、存在すれば所属を記録
					if _, ok := userTeamMap[login]; ok {
						userTeamMap[login][teamName] = true
					}
				}
				mapLock.Unlock() // アンロック

				if resp.NextPage == 0 {
					break
				}
				optTeamMember.Page = resp.NextPage
			}
		}(team)
	}

	wg.Wait()
	fmt.Printf("-> チーム所属メンバーの確認を完了しました。\n")

	// ----------------------------------------------------
	// 3. CSVに書き出し
	// ----------------------------------------------------
	
	file, err := os.Create(outputFile)
	if err != nil {
		log.Fatalf("CSVファイルの作成に失敗しました: %v", err)
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()

	// ユーザー名とチーム名でソート
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

	// ヘッダーを書き込み
	header := append([]string{"Login (ユーザー名)"}, teamNames...)
	writer.Write(header)

	// データ行を書き込み
	for _, login := range userLogins {
		row := []string{login}
		teamsBelonging := userTeamMap[login]
		for _, teamName := range teamNames {
			is_member := ""
			if teamsBelonging[teamName] {
				// 🌟 修正済み: "Yes" を "○" に変更 🌟
				is_member = "○"
			}
			row = append(row, is_member)
		}
		writer.Write(row)
	}

	fmt.Printf("\n✅ ユーザー → チームのマトリクスを '%s' に保存しました。\n", outputFile)
}