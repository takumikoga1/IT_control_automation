package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	// ↓↓↓ ここのタイポを修正しました ↓↓↓
	"github.com/joho/godotenv" 
)

// .env から読み込む設定を格納する構造体
type Config struct {
	GitHubToken string
	GitHubOwner string
	SinceDate   string
	UntilDate   string
	TargetRepos []string
}

// .env ファイルを読み込み、設定を構造体として返す
func loadConfig() Config {
	err := godotenv.Load()
	if err != nil {
		log.Println("警告: .env ファイルの読み込みに失敗しました。環境変数を直接使用します。")
	}

	reposStr := os.Getenv("TARGET_REPOS")
	if reposStr == "" {
		log.Fatal("エラー: .env に TARGET_REPOS が設定されていません。")
	}

	return Config{
		GitHubToken: os.Getenv("GITHUB_TOKEN"),
		GitHubOwner: os.Getenv("GITHUB_OWNER"),
		SinceDate:   os.Getenv("SINCE_DATE"),
		UntilDate:   os.Getenv("UNTIL_DATE"),
		TargetRepos: strings.Split(reposStr, ","),
	}
}

// checkTokenAndOrg は、指定されたトークンと組織名が有効かを確認する
func checkTokenAndOrg(token, owner string) error {
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN が設定されていません。")
	}
	if owner == "" {
		return fmt.Errorf("GITHUB_OWNER が設定されていません。")
	}

	fmt.Println("--- トークンと組織名の有効性を確認中... ---")

	apiURL := fmt.Sprintf("https://api.github.com/orgs/%s", owner)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("リクエストの作成に失敗しました: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("APIへのリクエストに失敗しました: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		fmt.Println("✅ トークンと組織名は有効です。")
		return nil // 成功
	case http.StatusNotFound:
		return fmt.Errorf("エラー: 組織名 '%s' が見つからないか、トークンにアクセス権がありません。(Status: 404)", owner)
	case http.StatusUnauthorized:
		return fmt.Errorf("エラー: GITHUB_TOKEN が無効です。(Status: 401)")
	default:
		return fmt.Errorf("エラー: GitHub APIから予期せぬ応答がありました。(Status: %d)", resp.StatusCode)
	}
}

// GitHub APIのレスポンスを格納する構造体
type CommitInfo struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
		Author  struct {
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

// CSVに出力する1行のデータを表す構造体
type CommitRecord struct {
	RepoName    string
	CommitDate  string
	Message     string
	SHA         string
	URL         string
}

func main() {
	cfg := loadConfig()

	if err := checkTokenAndOrg(cfg.GitHubToken, cfg.GitHubOwner); err != nil {
		log.Fatal(err) 
	}

	fmt.Println("\n--- 設定値に基づいてコミットの取得を開始します ---")
	fmt.Printf("OWNER: %s, SINCE: %s, UNTIL: %s\n", cfg.GitHubOwner, cfg.SinceDate, cfg.UntilDate)
	fmt.Println("-------------------------------------------------")

	allCommits := []CommitRecord{}
	client := &http.Client{}

	for _, repo := range cfg.TargetRepos {
		repoCommitsFound := 0
		
		fmt.Printf("\nリポジトリ '%s' のコミットを取得中...\n", repo)
		
		nextURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits?since=%s&until=%s&per_page=100", cfg.GitHubOwner, repo, cfg.SinceDate, cfg.UntilDate)
		
		for nextURL != "" {
			req, err := http.NewRequest("GET", nextURL, nil)
			if err != nil {
				log.Printf("リクエスト作成エラー (%s): %v\n", repo, err)
				break
			}

			req.Header.Set("Authorization", "Bearer "+cfg.GitHubToken)
			req.Header.Set("Accept", "application/vnd.github.v3+json")
			req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

			resp, err := client.Do(req)
			if err != nil {
				log.Printf("リクエスト送信エラー (%s): %v\n", repo, err)
				break
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				log.Printf("APIエラー (%s): ステータスコード %d。リポジトリ名を確認してください。\n", repo, resp.StatusCode)
				break
			}
			
			var commits []CommitInfo
			if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
				log.Printf("JSONデコードエラー (%s): %v\n", repo, err)
				break
			}
			
			if len(commits) == 0 && repoCommitsFound == 0 {
				break
			}
			
			repoCommitsFound += len(commits)

			for _, c := range commits {
				record := CommitRecord{
					RepoName:    repo,
					CommitDate:  c.Commit.Author.Date.Format(time.RFC3339),
					Message:     c.Commit.Message,
					SHA:         c.SHA,
					URL:         c.HTMLURL,
				}
				allCommits = append(allCommits, record)
			}
			
			nextURL = getNextPageURL(resp.Header.Get("Link"))
		}
		fmt.Printf("'%s' の結果: %d 件のコミットが見つかりました。\n", repo, repoCommitsFound)
	}
	
	fmt.Println("\n-------------------------------------------------")
	if len(allCommits) == 0 {
		fmt.Println("⚠️ 全リポジトリを通してコミットが見つかりませんでした。CSVファイルはヘッダーのみの空ファイルとして出力されます。")
		fmt.Println("➡️ .env ファイルの SINCE_DATE/UNTIL_DATE の期間や、TARGET_REPOS の内容を確認してください。")
	} else {
		fmt.Printf("合計 %d 件のコミットを取得完了。CSVファイルに出力します。\n", len(allCommits))
	}
	
	writeToCSV(allCommits)
}

// Linkヘッダーから次のページのURLを抽出する関数
func getNextPageURL(linkHeader string) string {
	if linkHeader == "" {
		return ""
	}
	re := regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)
	matches := re.FindStringSubmatch(linkHeader)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// 取得したコミットデータをCSVファイルに書き込む関数
func writeToCSV(records []CommitRecord) {
	file, err := os.Create("commits.csv")
	if err != nil {
		log.Fatalf("CSVファイルの作成に失敗しました: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()
	
	headers := []string{"No", "リポジトリ", "コミット日付", "コミット内容", "識別番号", "URL"}
	if err := writer.Write(headers); err != nil {
		log.Fatalf("ヘッダーの書き込みに失敗しました: %v", err)
	}
	
	for i, record := range records {
		row := []string{
			strconv.Itoa(i + 1),
			record.RepoName,
			record.CommitDate,
			record.Message,
			record.SHA,
			record.URL,
		}
		if err := writer.Write(row); err != nil {
			log.Printf("行の書き込みに失敗しました (SHA: %s): %v\n", record.SHA, err)
		}
	}
	
	fmt.Println("commits.csv の出力が完了しました。")
}