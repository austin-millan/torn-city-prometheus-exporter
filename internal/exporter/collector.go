package exporter

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"gitlab.com/torn-city/go-torn-client/client"
)

const namespace = "torn"

// Collector scrapes Torn account state and exposes it as Prometheus metrics.
type Collector struct {
	api      *client.API
	interval time.Duration
	logger   *slog.Logger

	mu        sync.RWMutex
	snapshot  *accountSnapshot
	lastError error
	lastFetch time.Time

	scrapeSuccess  prometheus.Gauge
	scrapeDuration prometheus.Gauge
	scrapeTimestamp prometheus.Gauge
	scrapeErrors   prometheus.Counter
}

type accountSnapshot struct {
	playerID   string
	playerName string
	gender     string
	level      float64
	ageDays    float64

	statusState string
	statusColor string
	statusUntil float64

	bars map[string]client.UserBar

	moneyWallet        float64
	moneyVault         float64
	moneyCayman        float64
	moneyCompany       float64
	moneyPoints        float64
	moneyDailyNetworth float64
	moneyCityBank      float64
	hasCityBank        bool

	cooldowns map[string]float64

	travelTimeLeft    float64
	travelDestination string
	travelMethod      string

	notifications map[string]float64

	refillEnergy  float64
	refillNerve   float64
	refillToken   float64
	refillSpecial float64

	battleStats map[string]float64

	networthTotal      float64
	networthComponents map[string]float64

	jobType     string
	jobName     string
	jobPosition string

	chainCurrent  float64
	chainTimeout  float64
	chainCooldown  float64
	chainModifier float64
	hasChain      bool
}

// NewCollector builds a Collector. Call Start to begin background refresh.
func NewCollector(api *client.API, interval time.Duration, logger *slog.Logger) *Collector {
	if interval <= 0 {
		interval = time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Collector{
		api:      api,
		interval: interval,
		logger:   logger,
		scrapeSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "scrape_success",
			Help:      "1 if the last Torn scrape succeeded, else 0.",
		}),
		scrapeDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "scrape_duration_seconds",
			Help:      "Duration of the last Torn scrape.",
		}),
		scrapeTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "scrape_timestamp_seconds",
			Help:      "Unix timestamp of the last successful Torn scrape.",
		}),
		scrapeErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "scrape_errors_total",
			Help:      "Total number of failed Torn scrapes.",
		}),
	}
}

// Describe implements prometheus.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	c.scrapeSuccess.Describe(ch)
	c.scrapeDuration.Describe(ch)
	c.scrapeTimestamp.Describe(ch)
	c.scrapeErrors.Describe(ch)
}

// Collect implements prometheus.Collector.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	snap := c.snapshot
	err := c.lastError
	c.mu.RUnlock()

	c.scrapeSuccess.Collect(ch)
	c.scrapeDuration.Collect(ch)
	c.scrapeTimestamp.Collect(ch)
	c.scrapeErrors.Collect(ch)

	if snap == nil {
		if err != nil {
			ch <- prometheus.NewInvalidMetric(
				prometheus.NewDesc(namespace+"_collector", "Torn collector error", nil, nil),
				err,
			)
		}
		return
	}

	labels := prometheus.Labels{
		"player_id":   snap.playerID,
		"player_name": snap.playerName,
	}

	ch <- gauge("player_info", "Torn player identity labels.", 1, labels, "gender", snap.gender)
	ch <- gauge("player_level", "Player level.", snap.level, labels)
	if snap.ageDays > 0 {
		ch <- gauge("player_age_days", "Player age in days.", snap.ageDays, labels)
	}

	ch <- gauge("status", "Player status (1 for the active state).", 1, labels, "state", snap.statusState, "color", snap.statusColor)
	if snap.statusUntil > 0 {
		ch <- gauge("status_until_timestamp_seconds", "Unix timestamp when the current status ends, if known.", snap.statusUntil, labels)
	}

	for barName, bar := range snap.bars {
		barLabels := withLabel(labels, "bar", barName)
		ch <- gauge("bar_current", "Current bar value.", float64(bar.Current), barLabels)
		ch <- gauge("bar_maximum", "Maximum bar value.", float64(bar.Maximum), barLabels)
		ch <- gauge("bar_full_time_seconds", "Seconds until the bar is full.", float64(bar.FullTime), barLabels)
		ch <- gauge("bar_tick_time_seconds", "Seconds until the next bar tick.", float64(bar.TickTime), barLabels)
		ch <- gauge("bar_increment", "Bar increment per tick.", float64(bar.Increment), barLabels)
		ch <- gauge("bar_interval_seconds", "Seconds between bar ticks.", float64(bar.Interval), barLabels)
	}

	money := map[string]float64{
		"wallet":         snap.moneyWallet,
		"vault":          snap.moneyVault,
		"cayman_bank":    snap.moneyCayman,
		"company":        snap.moneyCompany,
		"points":         snap.moneyPoints,
		"daily_networth": snap.moneyDailyNetworth,
	}
	if snap.hasCityBank {
		money["city_bank"] = snap.moneyCityBank
	}
	for account, value := range money {
		ch <- gauge("money", "Money and points by account bucket.", value, withLabel(labels, "account", account))
	}

	for kind, seconds := range snap.cooldowns {
		ch <- gauge("cooldown_seconds", "Remaining cooldown in seconds.", seconds, withLabel(labels, "type", kind))
	}

	ch <- gauge("travel_time_left_seconds", "Seconds remaining in travel, or 0 if not traveling.", snap.travelTimeLeft, labels)
	if snap.travelDestination != "" {
		ch <- gauge("travel_info", "Current travel destination and method.", 1, labels,
			"destination", snap.travelDestination,
			"method", emptyAs(snap.travelMethod, "none"),
		)
	}

	for kind, count := range snap.notifications {
		ch <- gauge("notifications", "Unread notification counts.", count, withLabel(labels, "type", kind))
	}

	ch <- gauge("refill_available", "Whether a daily refill is still available (1/0).", snap.refillEnergy, labels, "type", "energy")
	ch <- gauge("refill_available", "Whether a daily refill is still available (1/0).", snap.refillNerve, labels, "type", "nerve")
	ch <- gauge("refill_available", "Whether a daily refill is still available (1/0).", snap.refillToken, labels, "type", "token")
	ch <- gauge("refill_special_count", "Special refill count remaining.", snap.refillSpecial, labels)

	for stat, value := range snap.battleStats {
		ch <- gauge("battlestat", "Battle stat values.", value, withLabel(labels, "stat", stat))
	}

	ch <- gauge("networth_total", "Total networth.", snap.networthTotal, labels)
	for name, value := range snap.networthComponents {
		ch <- gauge("networth", "Networth component values.", value, withLabel(labels, "component", name))
	}

	if snap.jobType != "" || snap.jobName != "" {
		ch <- gauge("job_info", "Current job identity.", 1, labels,
			"type", emptyAs(snap.jobType, "none"),
			"name", emptyAs(snap.jobName, "none"),
			"position", emptyAs(snap.jobPosition, "none"),
		)
	}

	if snap.hasChain {
		ch <- gauge("chain_current", "Current faction chain length from bars.", snap.chainCurrent, labels)
		ch <- gauge("chain_timeout_seconds", "Seconds until the chain breaks.", snap.chainTimeout, labels)
		ch <- gauge("chain_cooldown_timestamp_seconds", "Chain cooldown end timestamp.", snap.chainCooldown, labels)
		ch <- gauge("chain_modifier", "Current chain modifier.", snap.chainModifier, labels)
	}
}

// Start launches background scraping until ctx is cancelled.
func (c *Collector) Start(ctx context.Context) {
	c.refresh(ctx)
	ticker := time.NewTicker(c.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.refresh(ctx)
			}
		}
	}()
}

func (c *Collector) refresh(ctx context.Context) {
	start := time.Now()
	snap, err := c.fetch(ctx)
	elapsed := time.Since(start).Seconds()
	c.scrapeDuration.Set(elapsed)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastFetch = time.Now()
	if err != nil {
		c.lastError = err
		c.scrapeSuccess.Set(0)
		c.scrapeErrors.Inc()
		c.logger.Error("torn scrape failed", "err", err, "duration_seconds", elapsed)
		return
	}
	c.snapshot = snap
	c.lastError = nil
	c.scrapeSuccess.Set(1)
	c.scrapeTimestamp.Set(float64(time.Now().Unix()))
	c.logger.Info("torn scrape ok",
		"player", snap.playerName,
		"player_id", snap.playerID,
		"duration_seconds", elapsed,
	)
}

func (c *Collector) fetch(ctx context.Context) (*accountSnapshot, error) {
	basicResp, err := c.api.GetMyBasicInformationWithResponse(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("basic: %w", err)
	}
	if basicResp.JSON200 == nil {
		return nil, fmt.Errorf("basic: empty response")
	}
	profile := basicResp.JSON200.Profile

	barsResp, err := c.api.GetMyBarsWithResponse(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("bars: %w", err)
	}
	if barsResp.JSON200 == nil {
		return nil, fmt.Errorf("bars: empty response")
	}

	moneyResp, err := c.api.GetMyMoneyWithResponse(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("money: %w", err)
	}
	if moneyResp.JSON200 == nil {
		return nil, fmt.Errorf("money: empty response")
	}

	cooldownsResp, err := c.api.GetMyCooldownsWithResponse(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("cooldowns: %w", err)
	}
	if cooldownsResp.JSON200 == nil {
		return nil, fmt.Errorf("cooldowns: empty response")
	}

	travelResp, err := c.api.GetMyTravelInformationWithResponse(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("travel: %w", err)
	}
	if travelResp.JSON200 == nil {
		return nil, fmt.Errorf("travel: empty response")
	}

	notificationsResp, err := c.api.GetMyNotificationsWithResponse(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("notifications: %w", err)
	}
	if notificationsResp.JSON200 == nil {
		return nil, fmt.Errorf("notifications: empty response")
	}

	refillsResp, err := c.api.GetMyRefillsWithResponse(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("refills: %w", err)
	}
	if refillsResp.JSON200 == nil {
		return nil, fmt.Errorf("refills: empty response")
	}

	battleResp, err := c.api.GetMyBattlestatsWithResponse(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("battlestats: %w", err)
	}
	if battleResp.JSON200 == nil {
		return nil, fmt.Errorf("battlestats: empty response")
	}

	networthResp, err := c.api.GetMyNetworthWithResponse(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("networth: %w", err)
	}
	if networthResp.JSON200 == nil {
		return nil, fmt.Errorf("networth: empty response")
	}

	profileResp, err := c.api.GetMyProfileWithResponse(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	if profileResp.JSON200 == nil {
		return nil, fmt.Errorf("profile: empty response")
	}

	jobResp, err := c.api.GetMyJobWithResponse(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("job: %w", err)
	}

	bars := barsResp.JSON200.Bars
	money := moneyResp.JSON200.Money
	cooldowns := cooldownsResp.JSON200.Cooldowns
	travel := travelResp.JSON200.Travel
	notifications := notificationsResp.JSON200.Notifications
	refills := refillsResp.JSON200.Refills
	battlestats := battleResp.JSON200.Battlestats
	networth := networthResp.JSON200.Networth
	fullProfile := profileResp.JSON200.Profile

	snap := &accountSnapshot{
		playerID:   strconv.FormatInt(int64(profile.Id), 10),
		playerName: profile.Name,
		gender:     string(profile.Gender),
		level:      float64(profile.Level),
		ageDays:    float64(fullProfile.Age),

		statusState: statusState(profile.Status),
		statusColor: profile.Status.Color,
		statusUntil: optionalFloat(profile.Status.Until),

		bars: map[string]client.UserBar{
			"energy": bars.Energy,
			"nerve":  bars.Nerve,
			"happy":  bars.Happy,
			"life":   bars.Life,
		},

		moneyWallet:        float64(money.Wallet),
		moneyVault:         float64(money.Vault),
		moneyCayman:        float64(money.CaymanBank),
		moneyCompany:       float64(money.Company),
		moneyPoints:        float64(money.Points),
		moneyDailyNetworth: float64(money.DailyNetworth),

		cooldowns: map[string]float64{
			"drug":    float64(cooldowns.Drug),
			"medical": float64(cooldowns.Medical),
			"booster": float64(cooldowns.Booster),
		},

		travelTimeLeft:    float64(travel.TimeLeft),
		travelDestination: string(travel.Destination),

		notifications: map[string]float64{
			"messages":    float64(notifications.Messages),
			"events":      float64(notifications.Events),
			"awards":      float64(notifications.Awards),
			"competition": float64(notifications.Competition),
		},

		refillEnergy:  boolFloat(refills.Energy),
		refillNerve:   boolFloat(refills.Nerve),
		refillToken:   boolFloat(refills.Token),
		refillSpecial: float64(refills.SpecialCount),

		battleStats: map[string]float64{
			"strength":  float64(battlestats.Strength.Value),
			"defense":   float64(battlestats.Defense.Value),
			"speed":     float64(battlestats.Speed.Value),
			"dexterity": float64(battlestats.Dexterity.Value),
			"total":     float64(battlestats.Total),
		},

		networthTotal: float64(networth.Total),
		networthComponents: map[string]float64{
			"money_wallet":       float64(networth.Money.Wallet),
			"money_vault":        float64(networth.Money.Vault),
			"money_cayman_bank": float64(networth.Money.CaymanBank),
			"money_city_bank":    float64(networth.Money.CityBank),
			"money_bookie":       float64(networth.Money.Bookie),
			"money_pending":      float64(networth.Money.Pending),
			"money_piggy_bank":   float64(networth.Money.PiggyBank),
			"money_loans":        float64(networth.Money.Loans),
			"money_unpaid_fees":  float64(networth.Money.UnpaidFees),
			"items_inventory":    float64(networth.Items.Inventory),
			"items_display_case": float64(networth.Items.DisplayCase),
			"items_bazaar":       float64(networth.Items.Bazaar),
			"items_trades":       float64(networth.Items.Trades),
			"items_item_market":  float64(networth.Items.ItemMarket),
			"items_auction":      float64(networth.Items.AuctionHouse),
			"items_cars":         float64(networth.Items.EnlistedCars),
			"assets_property":    float64(networth.Assets.Property),
			"assets_stock":       float64(networth.Assets.StockMarket),
			"assets_company":     float64(networth.Assets.Company),
			"points":             float64(networth.Points),
		},
	}

	if money.CityBank != nil {
		snap.hasCityBank = true
		snap.moneyCityBank = float64(money.CityBank.Amount)
	}
	if travel.Method != nil {
		snap.travelMethod = string(*travel.Method)
	}
	if bars.Chain != nil {
		snap.hasChain = true
		snap.chainCurrent = float64(bars.Chain.Current)
		snap.chainTimeout = float64(bars.Chain.Timeout)
		snap.chainCooldown = float64(bars.Chain.Cooldown)
		snap.chainModifier = float64(bars.Chain.Modifier)
	}
	if jobResp != nil && jobResp.JSON200 != nil {
		applyJob(snap, jobResp.JSON200)
	}

	return snap, nil
}
