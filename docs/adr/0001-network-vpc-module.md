# 0001. Mạng dùng module `terraform-aws-modules/vpc`, dựng mới từ đầu

- **Ngày:** 2026-11-20
- **Trạng thái:** Đã chốt **[user]**
- **Domain SAA-C03:** 3 — Design High-Performing Architectures (networking); liên quan 1 (VPC security)
- **Spec:** infra design §5 (mạng), §9.1 ("ưu tiên module `terraform-aws-modules/*`")

## Bối cảnh

Cách làm của dự án: **lab tay trên console → cleanup → Terraform dựng mới**. Mạng gồm 1 VPC
`/16`, 6 subnet (public/private/db × 2 AZ), internet gateway, route table cho từng tầng, S3
gateway endpoint. NAT nằm ở stack ephemeral.

## Các phương án

| Phương án | Chi phí/tháng | Ưu | Nhược |
|---|---|---|---|
| A. Resource thuần (`aws_vpc`, `aws_subnet`, ...) | $0 | Thấy rõ từng tài nguyên | ~15 resource tự viết; ngược spec §9.1 |
| B. Module `terraform-aws-modules/vpc` 6.7.3 | $0 | ~20 dòng; đúng spec; phổ biến trong thực tế; có sẵn khoá default SG, DB subnet group, tag cho Load Balancer Controller | Logic ẩn trong module, phải đọc input/output của module |

## Quyết định

Chọn **B**, ghim phiên bản `6.7.3`. S3 gateway endpoint viết bằng một resource `aws_vpc_endpoint`.

## Lý do

- Đúng spec §9.1, và module là cách làm phổ biến ở công ty.
- Dựng mới từ đầu (không import) nên không vướng việc khớp địa chỉ bên trong module.
- Lab tay trước đó đã giúp hiểu từng tài nguyên module tạo ra.

## Hệ quả

- `enable_nat_gateway = false` nhưng `single_nat_gateway = true`: module chỉ tạo **một** route
  table private dùng chung; stack `nat` thêm route `0.0.0.0/0` vào đó bằng `aws_route` riêng.
- `create_database_subnet_route_table = true`: subnet DB có route table riêng, chỉ có `local`,
  không bao giờ nhận route NAT.
- Module mặc định quản default security group/NACL/route table và **xoá hết rule của default SG**
  (thực hành CIS) — không gì dùng nhầm được default SG.
- Không đặt được `prevent_destroy` trên tài nguyên bên trong module; bảo vệ bằng review plan
  (PR) và quyền `tf-apply` cần duyệt tay.
- Nâng phiên bản module = một PR riêng, đọc kỹ plan.
