# 1. Tạo namespace monitoring
kubectl create namespace monitoring

# 2. Thêm Helm repo cho Grafana và cập nhật
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update

# 3. Cài đặt Loki Stack kèm file cấu hình (bật Grafana)
helm install loki-stack grafana/loki-stack --namespace monitoring -f loki-values.yaml

Write-Host "=========================================================="
Write-Host "✅ Đã gửi lệnh cài đặt Loki Stack lên K8s."
Write-Host "⏳ Vui lòng đợi khoảng 1-2 phút để các Pod khởi động xong."
Write-Host "Kiểm tra trạng thái Pod bằng lệnh: kubectl get pods -n monitoring"
Write-Host "=========================================================="
Write-Host "🔑 Dùng lệnh sau để lấy mật khẩu admin của Grafana (username mặc định là 'admin'):"
Write-Host 'kubectl get secret --namespace monitoring loki-stack-grafana -o jsonpath="{.data.admin-password}" | % { [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($_)) }'
Write-Host "=========================================================="
Write-Host "🌐 Sau đó, chạy lệnh này để mở Port Grafana ra ngoài:"
Write-Host "kubectl port-forward --namespace monitoring service/loki-stack-grafana 3000:80"
Write-Host "👉 Truy cập trình duyệt tại: http://localhost:3000"
Write-Host "=========================================================="
