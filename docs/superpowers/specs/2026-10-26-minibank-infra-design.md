# minibank — Thiết kế hạ tầng

**Ngày:** 2026-10-26 · **Trạng thái:** bản nháp, chờ duyệt
**Đi kèm:** [thiết kế ứng dụng](2026-10-26-minibank-app-design.md) — mục 12 của tài liệu đó là
danh sách yêu cầu tài liệu này phải đáp ứng.

Quy ước: **[user]** = người dùng đã chốt, **[claude]** = Claude đề xuất, có thể đổi khi review.

---

## 1. Mục tiêu và ràng buộc

- Chạy app minibank trên **EKS**, toàn bộ hạ tầng bằng **Terraform**, triển khai app bằng
  **GitOps (ArgoCD)** **[user]**.
- **Không chạy 24/7.** Bật khi làm/demo, tắt khi nghỉ. Dữ liệu và cấu hình phải còn nguyên giữa
  các buổi **[user]**.
- **Tối ưu chi phí trong ~6 tháng** và **học được nhiều nhất** — mỗi quyết định có ADR, gắn
  domain SAA-C03 **[user]**.
- Tham khảo series Vikki banking (devopsvn.tech), **thu nhỏ có chủ đích**: giữ khuôn tổ chức
  Terraform, Karpenter + Spot, CI chỉ chạy thư mục thay đổi; bỏ Transit Gateway, VPC
  ingress/egress riêng, Istio, 10 account **[user]**.
- Claude viết, người dùng review và tự commit/push.

**Không làm:** môi trường prod/uat (chỉ `dev`), multi-region, DR sang region khác, service mesh,
WAF ở đợt 1.

**Không được xảy ra:**
- Quên tắt cụm và bị tính tiền nhiều ngày.
- Credential AWS dài hạn nằm ở bất kỳ đâu (máy, CI, repo).
- Mất dữ liệu Aurora khi `destroy` stack ephemeral.

## 2. Tài khoản AWS **[user: 2 account]**

| Account | Chứa | Không chứa |
|---|---|---|
| `management` | AWS Organizations, IAM Identity Center, gom hoá đơn, Budgets, Cost Anomaly Detection, SCP | Không workload, không state, không CI |
| `workload` | Mọi tài nguyên của app, Terraform state, ECR, role OIDC cho CI | — |

Người dùng đăng nhập bằng IAM Identity Center (SSO). Không tạo IAM user nào **[claude]**.

**SCP gắn vào OU chứa `workload` [claude]:**
- Chỉ cho phép `ap-southeast-1`; ngoại lệ cho dịch vụ toàn cầu (IAM, CloudFront, ACM ở
  `us-east-1`, Route 53, Organizations, Budgets, STS, **Pricing API** — Karpenter cần nó để chọn
  instance rẻ). Đợt 2: nếu Bedrock phải dùng cross-region inference profile APAC thì nới SCP cho
  `bedrock:InvokeModel*` ở các region trong profile đó.
- Chặn instance lớn: **danh sách cho phép duy nhất** là `t4g`, `c6g`, `c7g`, `m6g`, `m7g`, tới cỡ
  `xlarge`. NodePool của Karpenter dùng đúng danh sách này (mục 6) — hai chỗ phải khớp, nếu
  không Karpenter sẽ gọi EC2 thất bại liên tục.
- Cấm tắt/xoá CloudTrail; cấm rời Organization.

Account `management` ít thay đổi → Terraform của nó do người dùng `apply` tay bằng SSO, không qua
CI **[claude]**. State của nó nằm trong **một bucket nhỏ riêng ở chính account `management`**
(vài cent/tháng) để khỏi phải cấp quyền chéo account. Ghi rõ trong README.

## 3. Region và tên miền

- `ap-southeast-1` (Singapore) **[user]**.
- **Tên miền: `andrewnguyen.cloud` [user]** — mua ở GoDaddy (2026-11-16), DNS ủy quyền về hosted
  zone public trong account `workload`. Zone tạo tay, Terraform sẽ `import` vào stack persistent;
  không xoá/tạo lại zone (NS sẽ đổi, phải sửa lại ở GoDaddy).

## 4. Hai stack: persistent và ephemeral

| Stack | Gồm | Vòng đời | Chi phí khi rảnh |
|---|---|---|---|
| **persistent** | VPC + subnet + S3 gateway endpoint, Aurora, Cognito, KMS, SNS/SQS, ECR, S3 + CloudFront, Secrets Manager, role OIDC cho CI, **mọi IAM role của workload** (service, ESO, Load Balancer Controller, Karpenter, Grafana Cloud) | Dựng một lần, giữ suốt | Vài USD/tháng |
| **ephemeral** | NAT instance, EKS + node group hệ thống, **Pod Identity association** (gắn role persistent vào service account), Karpenter interruption queue, bootstrap ArgoCD | `up` khi làm, `down` khi nghỉ | $0 |

Nguyên tắc: **ephemeral đọc output của persistent, không bao giờ ngược lại.** `down` không được
đụng tới bất kỳ tài nguyên persistent nào.

**Vì sao IAM role phải persistent:** key policy KMS và resource policy SQS/SNS lưu *ID nội bộ*
của role, không lưu tên. Role bị xoá rồi tạo lại cùng tên ở lần `up` sau sẽ mất quyền với khoá
KMS. Chỉ phần *gắn* role vào service account (Pod Identity association) là ephemeral.

## 5. Mạng

- VPC `/16`, 2 AZ (`ap-southeast-1a`, `-1b`) — bắt buộc vì EKS và Aurora cần subnet ở ≥ 2 AZ.
- Mỗi AZ: subnet public (`/24`), subnet private cho node và pod (`/19` — rộng vì VPC CNI cấp IP
  VPC cho từng pod), subnet DB (`/26`) **[claude]**.
- **NAT: 1 NAT instance fck-nat `t4g.nano` ở một AZ [user]**, trong stack ephemeral. Ghi ADR so
  sánh với NAT Gateway (giá theo giờ, phí theo GB, độ sẵn sàng).
- **S3 gateway endpoint** (miễn phí) để kéo layer image ECR và đọc S3 không qua NAT.
- Không dùng interface endpoint ở đợt 1 — mỗi cái ~$0.01/giờ/AZ, cộng lại đắt hơn NAT instance.
  Ghi ADR có con số.
- Aurora chỉ nằm trong subnet DB, không có đường ra Internet.

## 6. EKS và compute

- Phiên bản: bản mới nhất đang trong **standard support** lúc dựng; ghim cứng, nâng có chủ đích.
  Không để rơi vào extended support (đắt hơn nhiều lần).
- Quyền truy cập cụm: **EKS access entries**, không dùng `aws-auth` ConfigMap.
- **Endpoint API: public, mở cho mọi IP, xác thực hoàn toàn bằng IAM + access entries
  [claude].** Runner GitHub dùng hàng nghìn dải IP thay đổi, EKS chỉ cho khai ~40 CIDR, và đổi
  danh sách CIDR mỗi lần chạy tốn vài phút. Runner tự host trong VPC thì tốn tiền và vướng
  "con gà – quả trứng" ở bước `up`. Với cụm dev không có dữ liệu (dữ liệu ở Aurora), đây là
  đánh đổi chấp nhận được — ghi ADR, nêu rõ rủi ro và phương án chặt hơn cho prod.
- Addon do EKS quản: VPC CNI (bật **prefix delegation** để node nhỏ chứa được nhiều pod),
  CoreDNS, kube-proxy, **EKS Pod Identity Agent**. Không cài EBS CSI — cụm không có state.
- **Node group hệ thống: 1 × `t4g.medium` On-Demand [claude]** — chạy Karpenter, CoreDNS, ArgoCD,
  các controller. Dev nên chấp nhận không HA cho tầng này; ghi ADR.
- **Karpenter + Graviton Spot [user]** cho mọi workload còn lại:
  - NodePool `arm64`, ưu tiên Spot, cho phép rơi về On-Demand khi hết Spot.
  - Họ instance: đúng danh sách SCP cho phép (mục 2), nhiều cỡ để Spot đa dạng.
  - EC2NodeClass gắn tag `project`, `env`, `stack=ephemeral` cho node để kiểm mồ côi được.
  - Giới hạn tổng CPU của NodePool (VD 16 vCPU) — chặn chi phí khi có lỗi scale.
  - Bật consolidation.
  - Xử lý Spot bị thu hồi bằng interruption queue (SQS) của chính Karpenter. Không cài Node
    Termination Handler.
- Mọi pod lấy quyền AWS qua **Pod Identity**. Không có access key, không dùng IRSA.

## 7. Đường vào

### 7.1 Đường đi

`Browser → CloudFront → (/: S3 SPA) | (/api, /ws: NLB → Envoy Gateway → service)`

- CloudFront (persistent) có hai origin: S3 (Origin Access Control, bucket không public) và NLB.
- **NLB** do **AWS Load Balancer Controller** tạo cho Service của Envoy Gateway (target kiểu IP),
  gắn tag `stack=ephemeral` qua cấu hình default tags của controller.
- Hai lớp chặn gọi thẳng NLB **[claude]**:
  1. Security group **riêng** của NLB chỉ cho phép prefix list origin-facing của CloudFront.
     Prefix list này tính ~55 rule trên hạn mức 60 rule của một security group — không gắn
     thêm rule nào khác vào SG đó.
  2. Prefix list cho phép *mọi* distribution CloudFront, kể cả của người khác. Vì vậy CloudFront
     gắn thêm **header bí mật** (`X-Origin-Verify`), Envoy từ chối request thiếu header này.
     Giá trị header nằm trong Secrets Manager.
- `/api/*`, `/ws`: cache policy `CachingDisabled` + **origin request policy chuyển tiếp mọi
  header của viewer** (`Authorization`, `X-Transaction-Token`). Cache policy không cache thì
  không khai header được — header phải đi qua origin request policy. HTTPRoute của Envoy khớp
  đúng giá trị `Host` mà CloudFront gửi sang.
- CloudFront cho phép WebSocket đi qua.
- Envoy Gateway kiểm access token Cognito bằng `SecurityPolicy`: JWT provider với JWKS của user
  pool (không cấu hình `aud`), kiểm `client_id` và `token_use` bằng **luật authorization theo
  claim** — JWT provider của Envoy chỉ kiểm được `aud`, không kiểm được `client_id`. Rate-limit
  cục bộ bằng `BackendTrafficPolicy`.

### 7.2 Vấn đề NLB đổi mỗi lần `up` — và vì sao cần tên miền

NLB do Load Balancer Controller tạo *sau khi* ArgoCD sync, nên DNS name của nó đổi mỗi lần
`up` và Terraform không biết trước.

Cách giải: CloudFront trỏ origin vào một **tên cố định** `origin.<domain>`. **external-dns**
trong cụm tự cập nhật bản ghi Route 53 đó về NLB mới. CloudFront (persistent) không bao giờ
phải sửa, giữ đúng nguyên tắc ephemeral không ghi vào persistent.

Có tên miền còn giải quyết lỗ hổng lớn hơn: đoạn CloudFront → NLB chạy qua Internet công cộng.
Không có tên miền thì không có chứng chỉ cho NLB, đoạn đó là HTTP trần — JWT và **PIN giao
dịch** đi dạng rõ. Với app ngân hàng, đó là điều không chấp nhận được.

| Hạng mục | Thiết kế |
|---|---|
| URL người dùng | `bank.<domain>` — chứng chỉ ACM ở `us-east-1` cho CloudFront |
| CloudFront → NLB | HTTPS tới `origin.<domain>` — chứng chỉ ACM ở `ap-southeast-1` gắn vào listener TLS của NLB |
| Bản ghi `origin.<domain>` | external-dns quản, chỉ được ghi đúng bản ghi đó |
| Cognito Managed Login | `auth.<domain>` (custom domain) |
| Route 53 hosted zone | persistent, ~$0.50/tháng |

**Tên miền: `andrewnguyen.cloud` (mục 3)** — `bank.`, `origin.`, `auth.andrewnguyen.cloud`.

## 8. Dữ liệu, bảo mật, nhắn tin (persistent)

- **Aurora PostgreSQL Serverless v2**: phiên bản 16.x có hỗ trợ min 0 ACU; min 0, **max 2 ACU**
  (chặn chi phí); bật IAM database authentication; mật khẩu master do RDS quản trong Secrets
  Manager; mã hoá at-rest; giữ backup 1 ngày; `deletion_protection = true`; tự tạm dừng sau
  5 phút không kết nối.
- **Bootstrap database [claude]:** tạo database + user IAM cho từng service (`security`,
  `account`, `notification`) bằng một Job Kubernetes chạy một lần (đọc mật khẩu master qua
  External Secrets), idempotent. Không dùng Terraform PostgreSQL provider — Aurora nằm trong
  subnet riêng, máy chạy Terraform không với tới được.
- **Cognito**: user pool tier Essentials (cần cho Managed Login), thuộc tính `name` bắt buộc;
  hai app client — `web` (authorization code + PKCE) và `e2e-test` (cho phép
  `ADMIN_USER_PASSWORD_AUTH`, chỉ dùng cho script test).
- **KMS**: khoá `ECC_NIST_P256` SIGN_VERIFY (token giao dịch) và khoá HMAC (pepper PIN). Key
  policy chỉ cho role của service `security` dùng: `Sign`, `GetPublicKey`, `GenerateMac`,
  `VerifyMac`. **Service không có quyền tạo khoá [claude].**
- **Khoá mới khi xoay do Terraform tạo [claude]:** khoá ký khai báo dạng danh sách
  (`signing_keys = ["2026-11", ...]`). Xoay khoá = một PR vào `minibank-infra` thêm phần tử,
  rồi lệnh quản trị của `security` đổi trạng thái khoá trong DB. Gỡ khoá cũ = PR xoá phần tử
  (KMS lên lịch xoá sau 7 ngày). Đặc quyền tạo khoá chỉ nằm ở CI hạ tầng. Khoá HMAC xoay theo
  cùng cách.
- **SNS** topic `transactions` → **SQS** `notification` (raw message delivery, DLQ, maxReceive 5).
  Mã hoá bằng khoá do AWS quản (SSE-SQS/SNS) để khỏi tốn khoá KMS riêng **[claude]**.
- **ECR**: một repo cho mỗi service; tag bất biến (git SHA); scan on push (basic, miễn phí);
  lifecycle giữ 20 image gần nhất.
- **Secrets Manager [user]**: mật khẩu master Aurora (RDS quản), token Grafana Cloud, credential
  GitHub App cho ArgoCD, giá trị header `X-Origin-Verify`. Pod đọc qua **External Secrets
  Operator** với Pod Identity.
- **Ngoại lệ "con gà – quả trứng" [claude]:** ArgoCD cần credential GitHub App để đọc repo gitops
  (private) *trước khi* nó cài được ESO. Vì vậy riêng credential này do `argocd-bootstrap`
  Terraform đọc từ Secrets Manager và tạo thẳng secret repo của ArgoCD. Hệ quả: giá trị nằm
  trong Terraform state — chấp nhận được vì state mã hoá trên S3 và chỉ role CI đọc được. Ghi ADR.

## 9. Ba repo **[user]**

| Repo | Chứa | Ai ghi |
|---|---|---|
| `minibank` | Code app, Dockerfile, test, tài liệu thiết kế, ADR | Claude viết, người dùng commit |
| `minibank-infra` | Terraform | Claude viết, người dùng commit; CI apply |
| `minibank-gitops` | Manifest Kubernetes, tag image | Người dùng; CI của `minibank` chỉ được bump tag |

### 9.1 Cấu trúc `minibank-infra` **[claude]**

```
bootstrap/                    # state bucket — chạy tay một lần, state local rồi chuyển lên S3
management/
  organization/               # OU, SCP, Identity Center, Budgets, Anomaly Detection
workload/
  dev/
    persistent/
      network/  data/  identity/  messaging/  edge/  registry/  ci-oidc/
    ephemeral/
      nat/  eks/  addons-iam/  argocd-bootstrap/
modules/                      # module tự viết nhỏ khi module cộng đồng không vừa
```

- Mỗi thư mục lá là một root module với **state riêng**; key state = đường dẫn thư mục (giống
  Vikki).
- Backend S3 với `use_lockfile = true` (Terraform ≥ 1.10), không DynamoDB.
- Ưu tiên module `terraform-aws-modules/*`; ghim phiên bản module và provider.
- Thứ tự phụ thuộc giữa các thư mục đọc qua `terraform_remote_state` hoặc data source; ghi rõ
  trong README.
- Tag bắt buộc cho mọi tài nguyên: `project=minibank`, `env=dev`, `stack=persistent|ephemeral`,
  `component=<thư mục>` — bật làm **cost allocation tags** để báo cáo chi phí theo thành phần.

### 9.2 Cấu trúc `minibank-gitops` **[claude]**

```
bootstrap/root-app.yaml       # Application gốc, Terraform tạo
apps/                         # app-of-apps: mỗi file là một Application
addons/                       # Helm values cho addon bên thứ ba
  karpenter/  aws-load-balancer-controller/  envoy-gateway/
  external-secrets/  grafana-alloy/  argocd/
services/                     # Kustomize cho app của mình
  security/  account/  notification/  frontend-config/
    base/  overlays/dev/
```

- ArgoCD tự quản chính nó sau bootstrap (Application `argocd` trong `addons/`).
- Addon bên thứ ba: Helm chart qua ArgoCD, gồm thêm **external-dns** (chỉ được ghi bản ghi
  `origin.<domain>`). App của mình: Kustomize base + overlay.
- **GitOps Bridge**: Terraform ghi giá trị cụm cần (URL queue, ARN topic, ID khoá KMS, endpoint
  Aurora, ID Cognito, tên cụm, VPC ID) vào annotation của cluster secret trong ArgoCD;
  ApplicationSet/Application đọc ra. Không có giá trị AWS nào chép tay vào repo gitops.
- Job migration của từng service chạy như hook `PreSync` của ArgoCD.

### 9.3 Luồng triển khai app

1. Commit vào `minibank` → CI: lint, test, build image đa kiến trúc, Trivy quét image (chặn
   CRITICAL có bản vá), đẩy ECR với tag = git SHA.
2. Khi lên nhánh `main`: CI tạo commit bump tag trong `minibank-gitops/services/*/overlays/dev`
   bằng **token GitHub App** có quyền ghi duy nhất repo đó.
3. ArgoCD sync.
4. Frontend: CI build SPA, đồng bộ lên S3, invalidate CloudFront — không đi qua ArgoCD.

## 10. CI hạ tầng (`minibank-infra`)

- GitHub Actions xác thực AWS bằng **OIDC**. Các role ở `workload` **[claude]**:

  | Role | Quyền | Tin ai |
  |---|---|---|
  | `tf-plan` | Chỉ đọc + lock state | PR của `minibank-infra` |
  | `tf-apply` | Ghi stack persistent | Job dùng GitHub Environment `dev` (có duyệt tay) của `minibank-infra` |
  | `lifecycle` | Apply/destroy stack ephemeral, giới hạn theo tag `stack=ephemeral` khi được | Đúng các workflow `up`/`down`/tự tắt trên `main` của `minibank-infra`, **không** cần duyệt — tự tắt lúc 01:00 không có ai bấm duyệt |
  | `app-ci` | Đẩy ECR, sync bucket SPA, invalidate CloudFront | `main` của `minibank` |

- **Lưu ý OIDC:** khi job dùng `environment:`, claim `sub` của token GitHub thành
  `repo:<owner>/<repo>:environment:dev`, không còn chứa nhánh. Trust policy phải viết theo
  đúng dạng đó (hoặc tuỳ biến template `sub`, hoặc thêm điều kiện trên claim
  `job_workflow_ref`). Có test: thử assume từ nhánh khác phải thất bại.
- `ducnghiaaa` là tài khoản cá nhân; GitHub App cài vào tài khoản cá nhân, chỉ cấp cho repo
  `minibank-gitops`.
- **Trên PR:** `fmt`, `validate`, `tflint`, `trivy config` (chặn HIGH/CRITICAL), `plan` cho
  **chỉ những thư mục thay đổi** thuộc persistent, **Infracost** comment chênh lệch chi phí vào
  PR. Thư mục cần cụm đang chạy (`argocd-bootstrap`) không `plan` trên PR — chỉ `validate`.
- **Merge vào `main`:** `apply` các thư mục persistent thay đổi, có duyệt tay.
- Thư mục ephemeral không tự apply khi merge — chỉ chạy qua `up`/`down` (mục 11).
- **Drift:** workflow chạy `plan` hằng tuần cho stack persistent, báo nếu có khác biệt.

## 11. Vòng đời `up` / `down` và bảo vệ chi phí **[user]**

- **`up`** (workflow chạy tay, role `lifecycle`): apply ephemeral theo thứ tự `nat → eks →
  addons-iam → argocd-bootstrap`, đợi ArgoCD báo app `Healthy` và `origin.<domain>` trỏ đúng NLB
  mới, in thời gian từ đầu đến cuối (chỉ số "dựng lại từ đầu").
- **`down`** (workflow chạy tay, role `lifecycle`): trước khi destroy, xoá các tài nguyên do
  controller trong cụm tạo ra (Service kiểu LoadBalancer → NLB và security group của Load
  Balancer Controller; NodePool → node của Karpenter) — nếu không, VPC/subnet bị kẹt và node
  Spot chạy mồ côi. Rồi destroy ephemeral theo thứ tự ngược.
- **Tự tắt — hai lớp [claude]:**
  1. Workflow GitHub theo lịch, 01:00 giờ Việt Nam, chạy `down` nếu cụm còn sống.
  2. **Lưới an toàn phía AWS**: GitHub có thể trễ/bỏ lịch và tự tắt lịch sau 60 ngày repo không
     hoạt động. Vì vậy EventBridge Scheduler lúc 02:00 kích hoạt một project **CodeBuild** (nằm
     ở persistent, dùng role `lifecycle`) chạy đúng script `down` nếu cụm còn tồn tại. Chi phí
     gần như $0 (vài phút build mỗi lần thật sự chạy).
- **Kiểm mồ côi:** sau `down`, liệt kê EC2, NLB, ENI, security group, EIP còn tag
  `stack=ephemeral` hoặc tag của Karpenter/Load Balancer Controller — có thì báo lỗi.
- **Budgets** (account `management`): ngân sách **$40/tháng**, cảnh báo email ở 50/80/100% thực
  tế và 100% dự báo **[claude]**. Bật Cost Anomaly Detection (miễn phí).

## 12. Observability **[user: Grafana Cloud]**

- Cài Grafana Alloy bằng chart `k8s-monitoring` của Grafana qua ArgoCD; token lấy từ Secrets
  Manager qua External Secrets.
- Thu metric cụm (kube-state-metrics, node-exporter, cAdvisor) **có chọn lọc** để vừa hạn mức
  series của free tier; nhận OTLP từ service; đọc log pod.
- Metric AWS (SQS, DLQ, Aurora ACU) qua tích hợp CloudWatch của Grafana Cloud — cần một role IAM
  cho Grafana Cloud assume.
- Cảnh báo tối thiểu đợt 1: DLQ có message, outbox tồn đọng, pod crashloop, cụm vẫn sống sau
  01:00.

## 13. Chi phí ước tính (kiểm lại bằng AWS Pricing Calculator)

| Khoản | Khi chạy (mỗi giờ) | Khi rảnh (mỗi tháng) |
|---|---|---|
| EKS control plane | ~$0.10 | $0 |
| Node hệ thống `t4g.medium` On-Demand | ~$0.04 | $0 |
| Node Spot Graviton (1–2 cái) | ~$0.02–0.04 | $0 |
| NAT instance `t4g.nano` | ~$0.005 | $0 |
| NLB | ~$0.025 + LCU | $0 |
| IPv4 công khai (EIP của NAT + 2 IP của NLB) | ~$0.015 | $0 |
| Aurora (0.5–2 ACU khi chạy; giá ACU ở Singapore cao hơn us-east-1) | ~$0.08–0.35 | lưu trữ ~$1 |
| KMS (2 khoá), Secrets Manager (4) | — | ~$3.6 |
| Route 53 hosted zone | — | $0.50 (+ tiền tên miền theo năm) |
| Cognito, SNS/SQS, ECR, S3, CloudFront, CodeBuild | ~0 ở mức demo | < $1 |
| **Tổng** | **~$0.28–0.57/giờ** | **~$6/tháng** |

Lưu ý thêm: instance `t4g` mặc định ở chế độ *unlimited* — dùng CPU vượt mức credit kéo dài sẽ
tính thêm tiền. Theo dõi trong báo cáo chi phí.

Bật 30 giờ/tuần → khoảng **$40–80/tháng**. Ngân sách $40 sẽ báo động ở tháng làm nhiều — đó là
ý đồ: buộc nhìn lại. Người dùng có thể nâng ngưỡng.

## 14. Tiêu chí nghiệm thu đợt 1 (phần hạ tầng)

1. `up` từ con số 0 tới app `Healthy` chạy được không cần thao tác tay giữa chừng; đo thời gian.
2. `down` xong không còn tài nguyên mồ côi; dữ liệu Aurora còn nguyên ở lần `up` sau.
3. Không có access key AWS dài hạn nào tồn tại (kiểm IAM credential report).
4. PR vào `minibank-infra` có plan, kết quả quét, comment Infracost.
5. Một commit app đi tới cụm mà không có bước tay.
6. Tự tắt lúc 01:00 đã được thử ít nhất một lần.

## 15. Để lại cho người build

- Phiên bản cụ thể của Terraform, provider, module, chart; giá trị Helm chi tiết.
- Tên tài nguyên (theo khuôn `minibank-dev-<component>`).
- Cách viết workflow cụ thể, miễn đạt mục 10–11.
- Thứ tự ADR và nội dung chi tiết.
