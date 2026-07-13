package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"pulsar/internal/billing"
	"pulsar/internal/config"
	"pulsar/internal/models"
	"pulsar/internal/repository"
	"pulsar/web/views/pages"
)

// BillingHandler serves the billing page and payment-provider checkout redirects.
type BillingHandler struct {
	cfg       *config.Config
	yookassa  *billing.YooKassaService
	cryptobot *billing.CryptoBotService
	subs      *repository.SubscriptionsRepo
	plans     *repository.PlansRepo
	usage     *repository.UsageRepo
}

// NewBillingHandler wires dependencies.
func NewBillingHandler(
	cfg *config.Config,
	yk *billing.YooKassaService,
	cb *billing.CryptoBotService,
	subs *repository.SubscriptionsRepo,
	plans *repository.PlansRepo,
	usage *repository.UsageRepo,
) *BillingHandler {
	return &BillingHandler{
		cfg:       cfg,
		yookassa:  yk,
		cryptobot: cb,
		subs:      subs,
		plans:     plans,
		usage:     usage,
	}
}

// Routes registers billing routes (auth-protected).
func (h *BillingHandler) Routes(r chi.Router) {
	r.Get("/billing", h.show)
	// YooKassa
	r.Post("/billing/subscribe/yookassa", h.subscribeYooKassa)
	// CryptoBot
	r.Post("/billing/subscribe/cryptobot", h.subscribeCryptoBot)
}

func (h *BillingHandler) show(w http.ResponseWriter, r *http.Request) {
	uid := mustUserID(r)
	sub, err := h.subs.FindByUser(r.Context(), uid)
	if err != nil {
		// Defensive: should not happen because the trigger assigns a free plan.
		sub = &models.Subscription{PlanSlug: "free", Status: models.SubStatusActive}
	}
	usage, _ := h.usage.Summary(r.Context(), uid)
	plans, _ := h.plans.All(r.Context())
	viewPlans := toViewPlans(plans)
	props := baseProps(h.cfg, r, "Тариф и оплата", "", "billing")
	stripeEnabled := false
	ykEnabled := h.yookassa != nil && h.yookassa.Enabled()
	cbEnabled := h.cryptobot != nil && h.cryptobot.Enabled()
	Render(w, r, 0, pages.BillingPage(props, sub, usage, viewPlans, stripeEnabled, ykEnabled, cbEnabled))
}


// subscribeYooKassa creates a YooKassa payment and redirects to the confirmation URL.
func (h *BillingHandler) subscribeYooKassa(w http.ResponseWriter, r *http.Request) {
	uid := mustUserID(r)
	plan := strings.TrimSpace(r.URL.Query().Get("plan"))
	interval := strings.TrimSpace(r.URL.Query().Get("interval"))
	if interval != "yearly" {
		interval = "monthly"
	}
	if h.yookassa == nil || !h.yookassa.Enabled() {
		http.Error(w, "YooKassa не настроена", http.StatusServiceUnavailable)
		return
	}

	amountRub := planPriceRub(plan, interval)
	returnURL := h.cfg.HTTP.PublicBaseURL + "/app/billing?upgraded=1"
	desc := fmt.Sprintf("Pulsar %s (%s)", plan, interval)

	payURL, err := h.yookassa.CreatePaymentURL(r.Context(), uid, plan, interval, amountRub, returnURL)
	if err != nil {
		http.Error(w, "не удалось создать платёж YooKassa: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = desc
	http.Redirect(w, r, payURL, http.StatusSeeOther)
}

// subscribeCryptoBot creates a CryptoBot invoice and redirects to the pay_url.
func (h *BillingHandler) subscribeCryptoBot(w http.ResponseWriter, r *http.Request) {
	uid := mustUserID(r)
	plan := strings.TrimSpace(r.URL.Query().Get("plan"))
	interval := strings.TrimSpace(r.URL.Query().Get("interval"))
	asset := strings.TrimSpace(r.URL.Query().Get("asset")) // e.g. USDT, TON, BTC
	if interval != "yearly" {
		interval = "monthly"
	}
	if asset == "" {
		asset = billing.DefaultCryptoAsset
	}
	if h.cryptobot == nil || !h.cryptobot.Enabled() {
		http.Error(w, "CryptoBot не настроен", http.StatusServiceUnavailable)
		return
	}

	amount := planCryptoAmount(plan, interval, asset)
	desc := fmt.Sprintf("Pulsar %s (%s) — %s", plan, interval, asset)
	returnURL := h.cfg.HTTP.PublicBaseURL + "/app/billing?upgraded=1"

	payURL, err := h.cryptobot.CreateInvoice(r.Context(), uid, plan, interval, asset, amount, desc, returnURL)
	if err != nil {
		http.Error(w, "не удалось создать инвойс CryptoBot: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, payURL, http.StatusSeeOther)
}

// --- pricing helpers ---

// planPriceRub returns the plan price in whole rubles for YooKassa.
// Uses the same baseline prices as the web UI (cents / 100 = rubles, 1 USD ≈ 1 RUB simplified;
// in production these values should come from the DB plan rows via plans.FindBySlug).
func planPriceRub(planSlug, interval string) int64 {
	switch {
	case planSlug == "pro" && interval == "monthly":
		return 99 // 99 ₽/мес (≈ $0.99 — update to real price in production)
	case planSlug == "pro" && interval == "yearly":
		return 990
	case planSlug == "business" && interval == "monthly":
		return 499
	case planSlug == "business" && interval == "yearly":
		return 4990
	}
	return 0
}

// planCryptoAmount returns the invoice amount in the chosen crypto asset.
// In production, this should use a live exchange rate API.
// These are conservative placeholder values.
func planCryptoAmount(planSlug, interval, asset string) string {
	// Baseline in USDT — update to real prices.
	var usdBase float64
	switch {
	case planSlug == "pro" && interval == "monthly":
		usdBase = 9.90
	case planSlug == "pro" && interval == "yearly":
		usdBase = 99.00
	case planSlug == "business" && interval == "monthly":
		usdBase = 49.90
	case planSlug == "business" && interval == "yearly":
		usdBase = 499.00
	}
	switch strings.ToUpper(asset) {
	case "USDT":
		return fmt.Sprintf("%.2f", usdBase)
	case "TON":
		// 1 TON ≈ $5 — rough approximation, replace with live rate in production.
		return fmt.Sprintf("%.2f", usdBase/5.0)
	case "BTC":
		// 1 BTC ≈ $65000 — rough approximation.
		return fmt.Sprintf("%.8f", usdBase/65000.0)
	default:
		return fmt.Sprintf("%.2f", usdBase)
	}
}

// toViewPlans converts DB plans to the view model used by the pricing/billing pages.
func toViewPlans(plans []models.Plan) []pages.PlanViewModel {
	out := make([]pages.PlanViewModel, 0, len(plans))
	for _, p := range plans {
		hl := p.Slug == "pro"
		out = append(out, pages.PlanViewModel{
			Slug: p.Slug, Name: p.Name,
			PriceMonthly: p.PriceMonthlyCents, PriceYearly: p.PriceYearlyCents,
			StorageGB: p.StorageGB, BandwidthGBMonth: p.BandwidthGBMonth,
			MaxBuckets: p.MaxBuckets, CustomDomains: p.CustomDomainsAllowed,
			Highlighted: hl,
		})
	}
	return out
}

// keep chi referenced for future route expansions.
var _ = chi.NewRouter
