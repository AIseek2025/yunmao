package transport

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	yerr "yunmao.live/pkg/yunmao/errors"
	"yunmao.live/pkg/yunmao/httpx"
	"yunmao.live/pkg/yunmao/observability"

	"yunmao.live/services/billing-svc/internal/pay"
	"yunmao.live/services/billing-svc/internal/service"
)

// HandlerDeps transport 构造参数；PayRegistry 非空时挂载渠道路由。
type HandlerDeps struct {
	PayRegistry *pay.Registry
	// CredentialReadiness 非空时挂载 /internal/diagnose/credentials。
	CredentialReadiness *pay.CredentialReadiness
}

// New 构造 billing-svc HTTP handler；probes 非空时启用 /internal/readyz 深度探针。
func New(svc *service.BillingService, probes ...observability.Probes) http.Handler {
	return NewWithDeps(svc, HandlerDeps{}, probes...)
}

// NewWithDeps 完整构造；本轮（E）增加 pay 渠道路由。
func NewWithDeps(svc *service.BillingService, deps HandlerDeps, probes ...observability.Probes) http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.TraceMiddleware)
	p := observability.Probes{}
	for _, x := range probes {
		for k, v := range x {
			p[k] = v
		}
	}
	observability.WireFull(r, p, nil)

	if deps.CredentialReadiness != nil {
		r.Get("/internal/diagnose/credentials", diagnoseCredentials(deps.CredentialReadiness))
	}

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/orders", create(svc))
		r.Get("/orders/{id}", get(svc))
		r.Post("/orders/{id}/pay", markPaid(svc))
		r.Post("/orders/{id}/refund", refund(svc))

		// 第七轮（E）：渠道路由。
		if deps.PayRegistry != nil {
			r.Post("/orders/{id}/prepay", createPrepay(svc, deps.PayRegistry))
			r.Post("/pay/webhook/{channel}", payWebhook(svc, deps.PayRegistry))
			r.Post("/orders/{id}/refund/{channel}", channelRefund(svc, deps.PayRegistry))
			r.Get("/pay/channels", listChannels(deps.PayRegistry))
		}

		r.Post("/wallets/holds", reserveHold(svc))
		r.Post("/wallets/holds/{id}/confirm", confirmHold(svc))
		r.Post("/wallets/holds/{id}/cancel", cancelHold(svc))
		r.Get("/wallets/holds/{id}", getHold(svc))
		r.Get("/wallets/{user_id}", getWallet(svc))
		r.Post("/wallets/{user_id}/topup", topUp(svc))
	})
	return r
}

// 创建 prepay：根据 channel header / body 字段选渠道。
func createPrepay(svc *service.BillingService, reg *pay.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channel := r.Header.Get("X-Pay-Channel")
		if channel == "" {
			channel = r.URL.Query().Get("channel")
		}
		if channel == "" {
			channel = string(pay.ChannelMock)
		}
		ch, err := reg.Get(pay.Channel(channel))
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, err.Error()))
			return
		}
		var in pay.PrepayRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		in.OrderID = chi.URLParam(r, "id")
		// 校验订单存在 + amount 一致
		ord, err := svc.Get(r.Context(), in.OrderID)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if in.AmountFen == 0 {
			// amount_cny → fen
			in.AmountFen = int64(ord.AmountCny) * 100
		}
		out, err := ch.CreatePrepay(r.Context(), in)
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "prepay: "+err.Error()))
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, out)
	}
}

// 接收 webhook 回调：校验签名 → Confirm hold → 200 OK 幂等。
func payWebhook(svc *service.BillingService, reg *pay.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channel := chi.URLParam(r, "channel")
		ch, err := reg.Get(pay.Channel(channel))
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, err.Error()))
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "body too large"))
			return
		}
		// 把 request 头转为 map（保留我们关心的）。
		headers := map[string]string{}
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
		ev, err := ch.VerifyWebhook(r.Context(), raw, headers)
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "webhook: "+err.Error()))
			return
		}
		if ev.Status == "paid" {
			// 仅 paid 时推进订单状态；refund/closed 由专门路径处理。
			if _, mperr := svc.MarkPaid(r.Context(), ev.OrderID); mperr != nil {
				// 已 paid（幂等）时不阻塞响应；记录在日志即可。
				httpx.WriteJSON(w, http.StatusOK, map[string]any{
					"ok":     false,
					"reason": mperr.Error(),
					"event":  ev,
				})
				return
			}
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "event": ev})
	}
}

func channelRefund(svc *service.BillingService, reg *pay.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channel := chi.URLParam(r, "channel")
		ch, err := reg.Get(pay.Channel(channel))
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, err.Error()))
			return
		}
		var body pay.RefundRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		body.OrderID = chi.URLParam(r, "id")
		ord, err := svc.Get(r.Context(), body.OrderID)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if body.AmountFen == 0 {
			body.AmountFen = int64(ord.AmountCny) * 100
		}
		res, err := ch.Refund(r.Context(), body)
		if err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "refund: "+err.Error()))
			return
		}
		// 触发服务侧 Refund saga（cancel hold + outbox）。
		if _, srvErr := svc.Refund(r.Context(), body.OrderID); srvErr != nil {
			// 渠道已退款但内部 saga 失败需要补偿，这里只记录。
			httpx.WriteJSON(w, http.StatusOK, map[string]any{
				"ok":             false,
				"channel_refund": res,
				"saga_error":     srvErr.Error(),
			})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, res)
	}
}

// realModeReporter 渠道暴露 IsRealMode 时优先报告 mock/real。
type realModeReporter interface {
	IsRealMode() bool
}

func listChannels(reg *pay.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		names := reg.Names()
		out := make([]map[string]any, 0, len(names))
		for _, n := range names {
			ch, _ := reg.Get(n)
			info := map[string]any{
				"name": string(n),
				"mode": "mock",
			}
			if rep, ok := ch.(realModeReporter); ok && rep.IsRealMode() {
				info["mode"] = "real"
			}
			out = append(out, info)
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"channels": out})
	}
}

func reserveHold(svc *service.BillingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in service.WalletReserveInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		out, err := svc.Reserve(r.Context(), in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, out)
	}
}

func confirmHold(svc *service.BillingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.Confirm(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func cancelHold(svc *service.BillingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		out, err := svc.Cancel(r.Context(), chi.URLParam(r, "id"), body.Reason)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func getHold(svc *service.BillingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.GetHold(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func getWallet(svc *service.BillingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.Wallet(r.Context(), chi.URLParam(r, "user_id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func topUp(svc *service.BillingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AmountFen int64 `json:"amount_fen"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		if err := svc.TopUp(r.Context(), chi.URLParam(r, "user_id"), body.AmountFen); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func refund(svc *service.BillingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		o, err := svc.Refund(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, o)
	}
}

func create(svc *service.BillingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in service.CreateInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			httpx.WriteError(w, r, yerr.New(yerr.SystemInternal, "invalid json"))
			return
		}
		out, err := svc.Create(r.Context(), in)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, out)
	}
}

func get(svc *service.BillingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		o, err := svc.Get(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, o)
	}
}

func markPaid(svc *service.BillingService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		o, err := svc.MarkPaid(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, o)
	}
}

func diagnoseCredentials(cr *pay.CredentialReadiness) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out := map[string]any{
			"all_ready":     cr.AllReady(),
			"has_real_mode": cr.HasRealMode(),
			"checks":        cr.Checks,
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}
