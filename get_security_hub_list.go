package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/securityhub"
	"github.com/aws/aws-sdk-go-v2/service/securityhub/types"
	"github.com/joho/godotenv"
)

// Finding データ構造
type FindingDetail struct {
	Severity    string
	ID          string
	Description string
	Resource    string
}

// 検知内容の日本語マッピング
var findingTitleJapanese = map[string]string{
	// EC2関連
	"EC2.19 Security groups should not allow unrestricted access to ports with high risk": "EC2.19 セキュリティグループは高リスクポートへの無制限アクセスを許可すべきではありません",
	"EC2.18 Security groups should only allow unrestricted incoming traffic for authorized ports": "EC2.18 セキュリティグループは承認されたポートに対してのみ無制限の着信トラフィックを許可すべきです",
	"EC2.2 VPC default security groups should not allow inbound or outbound traffic": "EC2.2 VPCのデフォルトセキュリティグループはインバウンドまたはアウトバウンドトラフィックを許可すべきではありません",
	"4.1 Security groups should not allow ingress from 0.0.0.0/0 or ::/0 to port 22": "4.1 セキュリティグループは0.0.0.0/0または::/0からポート22への侵入を許可すべきではありません",
	"4.3 Ensure the default security group of every VPC restricts all traffic": "4.3 すべてのVPCのデフォルトセキュリティグループがすべてのトラフィックを制限することを確認してください",
	
	// S3関連
	"S3.2 S3 general purpose buckets should block public read access": "S3.2 S3汎用バケットはパブリック読み取りアクセスをブロックすべきです",
	"S3.8 S3 general purpose buckets should block public access": "S3.8 S3汎用バケットはパブリックアクセスをブロックすべきです",
	"S3.1 S3 general purpose buckets should have block public access settings enabled": "S3.1 S3汎用バケットはパブリックアクセスブロック設定を有効にすべきです",
	"S3.5 S3 general purpose buckets should require requests to use SSL": "S3.5 S3汎用バケットはリクエストでSSLの使用を要求すべきです",
	
	// SSM関連
	"SSM.7 SSM documents should have the block public sharing setting enabled": "SSM.7 SSMドキュメントはパブリック共有をブロックする設定を有効にすべきです",
	
	// ECR関連
	"ECR.1 ECR private repositories should have image scanning configured": "ECR.1 ECRプライベートリポジトリはイメージスキャンを設定すべきです",
	
	// Lambda関連
	"Lambda.1 Lambda function policies should prohibit public access": "Lambda.1 Lambda関数ポリシーはパブリックアクセスを禁止すべきです",
	"Lambda.2 Lambda functions should use supported runtimes": "Lambda.2 Lambda関数はサポートされているランタイムを使用すべきです",
	
	// RDS関連
	"RDS.1 RDS snapshot should be private": "RDS.1 RDSスナップショットはプライベートであるべきです",
	"RDS.2 RDS DB Instances should prohibit public access": "RDS.2 RDS DBインスタンスはパブリックアクセスを禁止すべきです",
	
	// IAM関連
	"IAM.1 IAM policies should not allow full '*' administrative privileges": "IAM.1 IAMポリシーは完全な'*'管理者権限を許可すべきではありません",
	"IAM.21 IAM customer managed policies that you create should not allow wildcard actions for services": "IAM.21 作成するIAMカスタマーマネージドポリシーは、サービスのワイルドカードアクションを許可すべきではありません",
	
	// CloudTrail関連
	"CloudTrail.1 CloudTrail should be enabled and configured with at least one multi-Region trail": "CloudTrail.1 CloudTrailを有効にし、少なくとも1つのマルチリージョントレイルで設定する必要があります",
	"CloudTrail.2 CloudTrail should have encryption at-rest enabled": "CloudTrail.2 CloudTrailは保管時の暗号化を有効にすべきです",
	
	// ElasticBeanstalk関連
	"ElasticBeanstalk.2 Elastic Beanstalk managed platform updates should be enabled": "ElasticBeanstalk.2 Elastic Beanstalkマネージドプラットフォームの更新を有効にすべきです",
	
	// ELB関連
	"ELB.2 Classic Load Balancers with SSL/HTTPS listeners should use a certificate provided by AWS Certificate Manager": "ELB.2 SSL/HTTPSリスナーを持つClassic Load Balancerは、AWS Certificate Managerが提供する証明書を使用すべきです",
	
	// APIGateway関連
	"APIGateway.1 API Gateway REST and WebSocket API execution logging should be enabled": "APIGateway.1 API Gateway RESTおよびWebSocket API実行ログを有効にすべきです",
	
	// Config関連
	"Config.1 AWS Config should be enabled": "Config.1 AWS Configを有効にすべきです",
	
	// KMS関連
	"KMS.4 AWS KMS key rotation should be enabled": "KMS.4 AWS KMSキーのローテーションを有効にすべきです",
	
	// Account関連
	"Account.1 Security contact information should be provided for an AWS account": "Account.1 AWSアカウントにセキュリティ連絡先情報を提供すべきです",
}

// タイトルを日本語に変換
func translateTitle(englishTitle string) string {
	if japanese, ok := findingTitleJapanese[englishTitle]; ok {
		return japanese
	}
	// マッピングにない場合は元のタイトルを返す
	return englishTitle
}

// 重大度の順序を返す
func getSeverityOrder(severity string) int {
	order := map[string]int{
		"CRITICAL":      0,
		"HIGH":          1,
		// "MEDIUM":        2,
		// "LOW":           3,
		// "INFORMATIONAL": 4,
	}
	if val, ok := order[severity]; ok {
		return val
	}
	return 999 // CRITICAL/HIGH以外は最後尾
}

// リソース情報をフォーマット
func formatResource(resource types.Resource) string {
	var parts []string

	// リソースタイプ
	if resource.Type != nil {
		parts = append(parts, *resource.Type)
	}

	// リソースID
	if resource.Id != nil {
		resourceID := *resource.Id
		
		// リソース詳細がある場合
		if resource.Details != nil {
			if resource.Details.AwsEc2SecurityGroup != nil &&
			   resource.Details.AwsEc2SecurityGroup.GroupName != nil {
				// セキュリティグループの場合
				groupName := *resource.Details.AwsEc2SecurityGroup.GroupName
				parts = append(parts, fmt.Sprintf("%s (%s)", resourceID, groupName))
			} else if resource.Details.AwsS3Bucket != nil &&
			          resource.Details.AwsS3Bucket.Name != nil {
				// S3バケットの場合
				bucketName := *resource.Details.AwsS3Bucket.Name
				parts = append(parts, bucketName)
			} else {
				parts = append(parts, resourceID)
			}
		} else {
			parts = append(parts, resourceID)
		}
	}

	return strings.Join(parts, "\n")
}

// 並列処理でSecurity Hubの検出結果を取得
func fetchFindings(ctx context.Context, client *securityhub.Client, workerCount int) ([]types.AwsSecurityFinding, error) {
	log.Println("Security Hubから検出結果を取得中...")
	startTime := time.Now()

	input := &securityhub.GetFindingsInput{
		Filters: &types.AwsSecurityFindingFilters{
			WorkflowStatus: []types.StringFilter{
				{Value: stringPtr("NEW"), Comparison: types.StringFilterComparisonEquals},
				{Value: stringPtr("NOTIFIED"), Comparison: types.StringFilterComparisonEquals},
			},
			// CRITICALとHIGHのみにフィルタリング
			SeverityLabel: []types.StringFilter{
				{Value: stringPtr("CRITICAL"), Comparison: types.StringFilterComparisonEquals},
				{Value: stringPtr("HIGH"), Comparison: types.StringFilterComparisonEquals},
			},
		},
		MaxResults: int32Ptr(100),
	}

	var allFindings []types.AwsSecurityFinding
	var findingsMux sync.Mutex
	var wg sync.WaitGroup
	var fetchErr error
	var errMux sync.Mutex

	tokenQueue := make(chan *string, 1000)
	var tokenQueueOpen sync.Mutex
	isQueueOpen := true

	tokenQueue <- nil

	var activeWorkers sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				tokenQueueOpen.Lock()
				if !isQueueOpen && len(tokenQueue) == 0 {
					tokenQueueOpen.Unlock()
					break
				}
				tokenQueueOpen.Unlock()

				var token *string
				var ok bool
				select {
				case token, ok = <-tokenQueue:
					if !ok {
						return
					}
				case <-time.After(2 * time.Second):
					return
				}

				errMux.Lock()
				if fetchErr != nil {
					errMux.Unlock()
					return
				}
				errMux.Unlock()

				activeWorkers.Add(1)
				pageInput := *input
				pageInput.NextToken = token

				resp, err := client.GetFindings(ctx, &pageInput)
				if err != nil {
					errMux.Lock()
					if fetchErr == nil {
						fetchErr = fmt.Errorf("worker %d error: %w", workerID, err)
					}
					errMux.Unlock()
					activeWorkers.Done()
					return
				}

				findingsMux.Lock()
				allFindings = append(allFindings, resp.Findings...)
				currentCount := len(allFindings)
				findingsMux.Unlock()

				log.Printf("Worker %d: 取得済み %d 件 (累計: %d 件)", workerID, len(resp.Findings), currentCount)

				if resp.NextToken != nil {
					tokenQueueOpen.Lock()
					if isQueueOpen {
						select {
						case tokenQueue <- resp.NextToken:
						default:
							log.Printf("警告: トークンキューが満杯です")
						}
					}
					tokenQueueOpen.Unlock()
				}

				activeWorkers.Done()
			}
		}(i)
	}

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			activeWorkers.Wait()

			tokenQueueOpen.Lock()
			if len(tokenQueue) == 0 && isQueueOpen {
				isQueueOpen = false
				close(tokenQueue)
				tokenQueueOpen.Unlock()
				return
			}
			tokenQueueOpen.Unlock()
		}
	}()

	wg.Wait()

	if fetchErr != nil {
		return nil, fetchErr
	}

	elapsed := time.Since(startTime)
	log.Printf("取得完了: %d 件 (所要時間: %s)", len(allFindings), elapsed)
	
	// デバッグ: 重大度別の件数を表示
	severityCounts := make(map[string]int)
	for _, f := range allFindings {
		if f.Severity != nil {
			severityCounts[string(f.Severity.Label)]++
		}
	}
	log.Println("=== 取得した検出結果の重大度別内訳 ===")
	for _, sev := range []string{"CRITICAL", "HIGH"} { // MEDIUM, LOW, INFORMATIONALをコメントアウト
		if count, ok := severityCounts[sev]; ok {
			log.Printf("  %s: %d件", sev, count)
		}
	}

	return allFindings, nil
}

// 検出結果を変換（全件を個別に出力）
func convertFindings(findings []types.AwsSecurityFinding) []FindingDetail {
	log.Println("検出結果を変換中...")

	details := make([]FindingDetail, 0, len(findings)*2)

	for _, finding := range findings {
		severity := ""
		if finding.Severity != nil && finding.Severity.Label != "" {
			severity = string(finding.Severity.Label)
		}

		// CRITICAL と HIGH のみ処理
		if severity != "CRITICAL" && severity != "HIGH" {
			continue
		}

		id := ""
		if finding.Id != nil {
			id = *finding.Id
		}

		description := ""
		if finding.Title != nil {
			// タイトルを日本語に変換
			description = translateTitle(*finding.Title)
		}

		// リソースがある場合は各リソースごとに行を作成
		if len(finding.Resources) > 0 {
			for _, resource := range finding.Resources {
				resourceStr := formatResource(resource)
				
				details = append(details, FindingDetail{
					Severity:    severity,
					ID:          id,
					Description: description,
					Resource:    resourceStr,
				})
			}
		} else {
			// リソースがない場合も1行作成
			details = append(details, FindingDetail{
				Severity:    severity,
				ID:          id,
				Description: description,
				Resource:    "",
			})
		}
	}

	// 重大度順にソート
	sort.Slice(details, func(i, j int) bool {
		severityOrderI := getSeverityOrder(details[i].Severity)
		severityOrderJ := getSeverityOrder(details[j].Severity)
		if severityOrderI != severityOrderJ {
			return severityOrderI < severityOrderJ
		}
		
		if details[i].Description != details[j].Description {
			return details[i].Description < details[j].Description
		}
		
		return details[i].ID < details[j].ID
	})

	log.Printf("変換完了: %d 件の検出結果を %d 行に展開", len(findings), len(details))

	return details
}

// CSV出力
func exportToCSV(details []FindingDetail, outputFile string) error {
	log.Printf("CSVファイルに出力中: %s", outputFile)

	outputDir := "/mnt/user-data/outputs"
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		outputFile = "./security_hub_findings.csv"
		log.Printf("出力先を変更: %s", outputFile)
	}

	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("ファイル作成エラー: %w", err)
	}
	defer file.Close()

	// UTF-8 BOMを追加
	file.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// ヘッダー行
	headers := []string{
		"重要度",
		"ID",
		"検知内容",
		"リソース",
	}
	if err := writer.Write(headers); err != nil {
		return fmt.Errorf("ヘッダー書き込みエラー: %w", err)
	}

	// データ行
	for _, detail := range details {
		record := []string{
			detail.Severity,
			detail.ID,
			detail.Description,
			detail.Resource,
		}
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("データ書き込みエラー: %w", err)
		}
	}

	log.Println("CSV出力完了")

	// 統計情報を表示
	severityCounts := make(map[string]int)
	titleCounts := make(map[string]int)
	
	for _, detail := range details {
		severityCounts[detail.Severity]++
		titleCounts[detail.Description]++
	}

	log.Println("\n=== CSV出力の重大度別件数 ===")
	for _, severity := range []string{"CRITICAL", "HIGH"} { // MEDIUM, LOW, INFORMATIONALをコメントアウト
		if count, exists := severityCounts[severity]; exists {
			log.Printf("  %s: %d件", severity, count)
		}
	}
	log.Printf("  合計: %d件", len(details))
	log.Printf("  ユニークな検知内容: %d種類\n", len(titleCounts))

	return nil
}

// AWS認証情報を設定からロード
func loadAWSConfig(ctx context.Context, region string) (aws.Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Printf("警告: .envファイルが見つかりません（環境変数から読み込みます）: %v", err)
	}

	accessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	sessionToken := os.Getenv("AWS_SESSION_TOKEN")

	var cfg aws.Config
	var err error

	if accessKeyID != "" && secretAccessKey != "" {
		log.Println("環境変数からAWS認証情報を読み込みました")

		credsProvider := credentials.NewStaticCredentialsProvider(
			accessKeyID,
			secretAccessKey,
			sessionToken,
		)

		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
			config.WithCredentialsProvider(credsProvider),
		)
	} else {
		log.Println("デフォルトのAWS認証情報プロバイダーを使用します")
		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
		)
	}

	if err != nil {
		return aws.Config{}, fmt.Errorf("AWS設定のロードに失敗: %w", err)
	}

	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return aws.Config{}, fmt.Errorf("AWS認証情報の取得に失敗: %w", err)
	}

	maskedKey := "****"
	if len(creds.AccessKeyID) > 8 {
		maskedKey = creds.AccessKeyID[:4] + "****" + creds.AccessKeyID[len(creds.AccessKeyID)-4:]
	}
	log.Printf("AWS認証情報: %s", maskedKey)

	return cfg, nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	ctx := context.Background()

	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "ap-northeast-1"
	}

	workerCount := 10
	if count := os.Getenv("WORKER_COUNT"); count != "" {
		fmt.Sscanf(count, "%d", &workerCount)
	}

	outputFile := os.Getenv("OUTPUT_FILE")
	if outputFile == "" {
		outputFile = "/mnt/user-data/outputs/security_hub_findings.csv"
	}

	log.Println("==========================================")
	log.Println("Security Hub 検出結果エクスポートツール (CRITICAL/HIGH のみ)")
	log.Println("==========================================")
	log.Printf("リージョン: %s", region)
	log.Printf("並列ワーカー数: %d", workerCount)
	log.Printf("出力ファイル: %s", outputFile)
	log.Println("==========================================\n")

	cfg, err := loadAWSConfig(ctx, region)
	if err != nil {
		log.Fatalf("❌ エラー: %v", err)
	}

	client := securityhub.NewFromConfig(cfg)

	findings, err := fetchFindings(ctx, client, workerCount)
	if err != nil {
		log.Fatalf("❌ 検出結果の取得に失敗: %v", err)
	}

	if len(findings) == 0 {
		log.Println("⚠️  CRITICAL/HIGH の検出結果が見つかりませんでした")
		return
	}

	details := convertFindings(findings)

	if err := exportToCSV(details, outputFile); err != nil {
		log.Fatalf("❌ CSV出力に失敗: %v", err)
	}

	log.Println("==========================================")
	log.Printf("✅ 処理完了! 出力ファイル: %s", outputFile)
	log.Println("==========================================")
}

func stringPtr(s string) *string {
	return &s
}

func int32Ptr(i int32) *int32 {
	return &i
}