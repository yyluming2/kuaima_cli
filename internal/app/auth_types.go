package app

type userLoginResponse struct {
	Message string `json:"message"`
	Success bool   `json:"success"`
	Data    struct {
		Token string `json:"token"`
	} `json:"data"`
}

type simpleAPIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type tokenUsage struct {
	Object         string              `json:"object"`
	Name           string              `json:"name"`
	Group          string              `json:"group"` //用户身份 default, vip, svip
	Subscriptions  []tokenSubscription `json:"subscriptions"`
	TotalGranted   int64               `json:"total_granted"`
	TotalUsed      int64               `json:"total_used"`
	TotalAvailable int64               `json:"total_available"`
	UnlimitedQuota bool                `json:"unlimited_quota"`
	ExpiresAt      int64               `json:"expires_at"`
}

type tokenSubscription struct {
	Subscription subscriptionPlan `json:"subscription"`
}

type subscriptionPlan struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"user_id"`
	PlanID        int64  `json:"plan_id"`
	AmountTotal   int64  `json:"amount_total"`
	AmountUsed    int64  `json:"amount_used"`
	StartTime     int64  `json:"start_time"`
	EndTime       int64  `json:"end_time"`
	Status        string `json:"status"`
	Source        string `json:"source"`
	LastResetTime int64  `json:"last_reset_time"`
	NextResetTime int64  `json:"next_reset_time"`
	UpgradeGroup  string `json:"upgrade_group"`
	PrevUserGroup string `json:"prev_user_group"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

type tokenUsageResponse struct {
	Code    bool       `json:"code"`
	Message string     `json:"message"`
	Data    tokenUsage `json:"data"`
}

type rechargeInfoResponse struct {
	Success bool         `json:"success"`
	Message string       `json:"message"`
	Data    rechargeInfo `json:"data"`
}

type rechargeInfo struct {
	AmountOptions []float64          `json:"amount_options"`
	Discount      map[string]float64 `json:"discount"`
	Plans         []rechargePlan     `json:"plans"`
}

type rechargePlan struct {
	ID                      int     `json:"id"`
	Title                   string  `json:"title"`
	Subtitle                string  `json:"subtitle"`
	PriceAmount             float64 `json:"price_amount"`
	Currency                string  `json:"currency"`
	DurationUnit            string  `json:"duration_unit"`
	DurationValue           int     `json:"duration_value"`
	CustomSeconds           int64   `json:"custom_seconds"`
	Enabled                 bool    `json:"enabled"`
	SortOrder               int     `json:"sort_order"`
	StripePriceID           string  `json:"stripe_price_id"`
	CreemProductID          string  `json:"creem_product_id"`
	MaxPurchasePerUser      int     `json:"max_purchase_per_user"`
	UpgradeGroup            string  `json:"upgrade_group"`
	TotalAmount             int64   `json:"total_amount"`
	QuotaResetPeriod        string  `json:"quota_reset_period"`
	QuotaResetCustomSeconds int64   `json:"quota_reset_custom_seconds"`
	CreatedAt               int64   `json:"created_at"`
	UpdatedAt               int64   `json:"updated_at"`
}

type paymentResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    paymentData `json:"data"`
}

type paymentData struct {
	ScanCodeURL string `json:"scancode_url"`
	TradeNo     string `json:"trade_no"`
}
