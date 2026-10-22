package main

import (
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"
)

// Keep the authority kernel deterministic and aligned with minigame/src/model.js.
// Numbers are teaching parameters; they are not calibrated farm prescriptions.
const vineyardOfflineDayMS int64 = 3600000
const vineyardSafeInteger int64 = 9007199254740991

type vineyardFarm struct {
	SchemaVersion int                `json:"schemaVersion"`
	RulesVersion  string             `json:"rulesVersion"`
	Season        int                `json:"season"`
	Day           int                `json:"day"`
	Version       int64              `json:"version"`
	Seed          uint32             `json:"seed"`
	Coins         int                `json:"coins"`
	TotalCosts    int                `json:"totalCosts"`
	TotalRevenue  int                `json:"totalRevenue"`
	LastSettledAt int64              `json:"lastSettledAt"`
	Plots         []vineyardPlot     `json:"plots"`
	Inventory     []vineyardBatch    `json:"inventory"`
	History       []vineyardHistory  `json:"history"`
	Report        *vineyardReport    `json:"report"`
	Facilities    vineyardFacilities `json:"facilities"`
	Stats         vineyardStats      `json:"stats"`
}
type vineyardPlot struct {
	ID          int                 `json:"id"`
	Name        string              `json:"name"`
	Moisture    float64             `json:"moisture"`
	Canopy      float64             `json:"canopy"`
	Load        float64             `json:"load"`
	Vigor       float64             `json:"vigor"`
	BerryWeight float64             `json:"berryWeight"`
	Brix        float64             `json:"brix"`
	Damage      float64             `json:"damage"`
	Fertility   float64             `json:"fertility"`
	Harvested   bool                `json:"harvested"`
	LastActions vineyardLastActions `json:"lastActions"`
}
type vineyardLastActions struct {
	Water     int `json:"water"`
	Fertilize int `json:"fertilize"`
	Prune     int `json:"prune"`
	Thin      int `json:"thin"`
	Protect   int `json:"protect"`
}
type vineyardBatch struct {
	ID            string  `json:"id"`
	PlotID        int     `json:"plotId"`
	Grade         string  `json:"grade"`
	Weight        int     `json:"weight"`
	InitialWeight int     `json:"initialWeight"`
	Brix          float64 `json:"brix"`
	HarvestDay    int     `json:"harvestDay"`
	HarvestSeason int     `json:"harvestSeason"`
}
type vineyardHistory struct {
	Day     int    `json:"day"`
	Season  int    `json:"season"`
	Type    string `json:"type"`
	Text    string `json:"text"`
	Cost    *int   `json:"cost,omitempty"`
	Revenue *int   `json:"revenue,omitempty"`
}
type vineyardReport struct {
	Season        int      `json:"season"`
	HarvestWeight int      `json:"harvestWeight"`
	SoldWeight    int      `json:"soldWeight"`
	SpoiledWeight int      `json:"spoiledWeight"`
	TotalRevenue  int      `json:"totalRevenue"`
	TotalCosts    int      `json:"totalCosts"`
	Profit        int      `json:"profit"`
	AvgBrix       float64  `json:"avgBrix"`
	BestGrade     string   `json:"bestGrade"`
	Notes         []string `json:"notes"`
}
type vineyardFacilities struct {
	Drip        bool `json:"drip"`
	Shelter     bool `json:"shelter"`
	ColdStorage bool `json:"coldStorage"`
}
type vineyardStats struct {
	HarvestWeight int `json:"harvestWeight"`
	SoldWeight    int `json:"soldWeight"`
	SpoiledWeight int `json:"spoiledWeight"`
}
type vineyardCommand struct {
	Type     string `json:"type"`
	PlotID   int    `json:"plotId,omitempty"`
	Channel  string `json:"channel,omitempty"`
	Facility string `json:"facility,omitempty"`
}
type vineyardStage struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Start, End int
}
type vineyardWeather struct {
	Kind                 string `json:"kind"`
	Name                 string `json:"name"`
	Rain, Temp, Humidity float64
}
type vineyardActionRule struct {
	Name                       string
	Cost, Start, End, Cooldown int
}
type vineyardFacilityRule struct {
	Name            string
	Cost, DailyCost int
}

var vineyardStages = []vineyardStage{{"dormant", "冬剪准备", 0, 6}, {"budbreak", "萌芽展叶", 7, 20}, {"flowering", "开花坐果", 21, 34}, {"enlargement", "果粒膨大", 35, 55}, {"ripening", "转色成熟", 56, 69}, {"harvest", "采收销售", 70, 83}, {"closed", "本季结束", 84, 84}}
var vineyardActionRules = map[string]vineyardActionRule{
	"water": {"灌溉", 70, 0, 83, 1}, "fertilize": {"施肥", 140, 0, 55, 7}, "prune": {"整枝通风", 110, 0, 55, 7},
	"thin": {"疏果控产", 130, 21, 55, 7}, "protect": {"巡园防护", 120, 21, 83, 7}, "harvest": {"采收", 220, 56, 83, 0},
}
var vineyardFacilityRules = map[string]vineyardFacilityRule{"drip": {"滴灌系统", 4200, 36}, "shelter": {"避雨设施", 6500, 12}, "coldStorage": {"小型冷库", 4800, 18}}

func vineyardClamp(value, minValue, maxValue float64) float64 {
	return math.Max(minValue, math.Min(maxValue, value))
}
func vineyardRound(value float64) float64 {
	return math.Floor((value+2.220446049250313e-16)*100+0.5) / 100
}
func vineyardNumber(value, minValue, maxValue float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minValue && value <= maxValue
}
func vineyardInt(value, minValue, maxValue int) bool { return value >= minValue && value <= maxValue }
func vineyardString(value string, maxLength int) bool {
	n := utf8.RuneCountInString(value)
	return n > 0 && n <= maxLength
}
func getVineyardStage(day int) vineyardStage {
	for _, stage := range vineyardStages {
		if day >= stage.Start && day <= stage.End {
			return stage
		}
	}
	return vineyardStage{}
}
func getVineyardWeather(seed uint32, day int) vineyardWeather {
	x := seed ^ (uint32(day+1) * uint32(0x9E3779B1))
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	value := float64(x) / 4294967296
	if value < 0.22 {
		return vineyardWeather{"rain", "持续降雨", 15, 23, 90}
	}
	if value < 0.38 {
		return vineyardWeather{"shower", "阵雨", 8, 25, 75}
	}
	if value < 0.78 {
		return vineyardWeather{"sunny", "晴朗", 0, 27, 48}
	}
	return vineyardWeather{"hot", "晴热", 0, 34, 35}
}
func makeVineyardPlot(id int, vigor float64) vineyardPlot {
	names := []string{"朝阳园", "溪边园", "南坡园", "老藤园"}
	return vineyardPlot{ID: id, Name: names[id-1], Moisture: 60, Canopy: 78, Load: 84, Vigor: vineyardRound(vigor), BerryWeight: 0.8, Brix: 5, Fertility: 58, LastActions: vineyardLastActions{-99, -99, -99, -99, -99}}
}
func createVineyardFarm(now int64, seed uint32) vineyardFarm {
	return vineyardFarm{SchemaVersion: 1, RulesVersion: "v1", Season: 1, Day: 0, Version: 0, Seed: seed, Coins: 18000, LastSettledAt: now,
		Plots: []vineyardPlot{makeVineyardPlot(1, 72), makeVineyardPlot(2, 74), makeVineyardPlot(3, 70), makeVineyardPlot(4, 76)}, Inventory: []vineyardBatch{}, History: []vineyardHistory{}}
}
func cloneVineyardFarm(state vineyardFarm) vineyardFarm {
	state.Plots = append([]vineyardPlot{}, state.Plots...)
	state.Inventory = append([]vineyardBatch{}, state.Inventory...)
	state.History = append([]vineyardHistory{}, state.History...)
	if state.Report != nil {
		copied := *state.Report
		copied.Notes = append([]string{}, copied.Notes...)
		state.Report = &copied
	}
	return state
}
func appendVineyardHistory(state *vineyardFarm, kind, text string, cost, revenue *int) {
	state.History = append(state.History, vineyardHistory{state.Day, state.Season, kind, text, cost, revenue})
	if len(state.History) > 120 {
		state.History = state.History[len(state.History)-120:]
	}
}
func vineyardIntPointer(value int) *int { return &value }

func validateVineyardSave(state vineyardFarm) bool {
	if state.SchemaVersion != 1 || state.RulesVersion != "v1" || !vineyardInt(state.Season, 1, 10000) || !vineyardInt(state.Day, 0, 84) || state.Version < 0 || state.Version > 1000000000 || state.LastSettledAt < 0 || state.LastSettledAt > vineyardSafeInteger {
		return false
	}
	if !vineyardInt(state.Coins, 0, 1000000000) || !vineyardInt(state.TotalCosts, 0, 1000000000) || !vineyardInt(state.TotalRevenue, 0, 1000000000) || len(state.Plots) != 4 || state.Inventory == nil || len(state.Inventory) > 4 || state.History == nil || len(state.History) > 120 {
		return false
	}
	ids := map[int]bool{}
	for _, plot := range state.Plots {
		if !vineyardInt(plot.ID, 1, 4) || ids[plot.ID] || !vineyardString(plot.Name, 24) {
			return false
		}
		ids[plot.ID] = true
		for _, value := range []float64{plot.Moisture, plot.Canopy, plot.Load, plot.Vigor, plot.Damage, plot.Fertility} {
			if !vineyardNumber(value, 0, 100) {
				return false
			}
		}
		if !vineyardNumber(plot.BerryWeight, 0, 12) || !vineyardNumber(plot.Brix, 0, 24) {
			return false
		}
		for _, day := range []int{plot.LastActions.Water, plot.LastActions.Fertilize, plot.LastActions.Prune, plot.LastActions.Thin, plot.LastActions.Protect} {
			if !vineyardInt(day, -99, state.Day) {
				return false
			}
		}
	}
	batchIDs := map[string]bool{}
	batchPlots := map[int]bool{}
	storedWeight := 0
	for _, batch := range state.Inventory {
		if !vineyardString(batch.ID, 40) || batchIDs[batch.ID] || !vineyardInt(batch.PlotID, 1, 4) || batchPlots[batch.PlotID] || (batch.Grade != "A" && batch.Grade != "B" && batch.Grade != "C") || !vineyardInt(batch.Weight, 1, 200000) || !vineyardInt(batch.InitialWeight, batch.Weight, 200000) || !vineyardNumber(batch.Brix, 15, 24) || !vineyardInt(batch.HarvestDay, 56, state.Day) || batch.HarvestSeason != state.Season {
			return false
		}
		var harvested bool
		for _, plot := range state.Plots {
			if plot.ID == batch.PlotID {
				harvested = plot.Harvested
			}
		}
		if !harvested {
			return false
		}
		batchIDs[batch.ID], batchPlots[batch.PlotID] = true, true
		storedWeight += batch.Weight
	}
	for _, entry := range state.History {
		if !vineyardInt(entry.Day, 0, 84) || !vineyardInt(entry.Season, 1, state.Season) || !vineyardString(entry.Type, 32) || !vineyardString(entry.Text, 300) || (entry.Cost != nil && !vineyardInt(*entry.Cost, 0, 1000000000)) || (entry.Revenue != nil && !vineyardInt(*entry.Revenue, 0, 1000000000)) {
			return false
		}
	}
	if !vineyardInt(state.Stats.HarvestWeight, 0, 800000) || !vineyardInt(state.Stats.SoldWeight, 0, 800000) || !vineyardInt(state.Stats.SpoiledWeight, 0, 800000) || state.Stats.HarvestWeight != state.Stats.SoldWeight+state.Stats.SpoiledWeight+storedWeight {
		return false
	}
	if state.Report != nil {
		report := state.Report
		if report.Season != state.Season || state.Day != 84 || report.Notes == nil || len(report.Notes) > 8 || !vineyardNumber(report.AvgBrix, 0, 24) || (report.BestGrade != "A" && report.BestGrade != "B" && report.BestGrade != "C" && report.BestGrade != "—") {
			return false
		}
		for _, note := range report.Notes {
			if !vineyardString(note, 300) {
				return false
			}
		}
		for _, value := range []int{report.HarvestWeight, report.SoldWeight, report.SpoiledWeight, report.TotalRevenue, report.TotalCosts} {
			if !vineyardInt(value, 0, 1000000000) {
				return false
			}
		}
		if !vineyardInt(report.Profit, -1000000000, 1000000000) {
			return false
		}
	}
	return true
}
func gradeVineyardPlot(plot vineyardPlot) string {
	if plot.Brix >= 17.2 && plot.BerryWeight >= 6 && plot.Damage <= 14 {
		return "A"
	}
	if plot.Brix >= 15 && plot.BerryWeight >= 4.5 && plot.Damage <= 30 {
		return "B"
	}
	return "C"
}
func getVineyardReport(state vineyardFarm) *vineyardReport {
	notes := []string{}
	harvested := 0
	brix := float64(0)
	bestGrade := "—"
	damaged, overloaded := false, false
	for _, plot := range state.Plots {
		if plot.Harvested {
			harvested++
			brix += plot.Brix
			grade := gradeVineyardPlot(plot)
			if bestGrade == "—" || grade < bestGrade {
				bestGrade = grade
			}
		}
		if plot.Damage > 20 {
			damaged = true
		}
		if plot.Load > 78 {
			overloaded = true
		}
	}
	if state.Stats.HarvestWeight == 0 {
		notes = append(notes, "本季没有商品果入库；成熟期留意糖度并及时采收。")
	}
	if damaged {
		notes = append(notes, "病害或水分波动降低了商品率；通风、防护与稳定供水需要配合。")
	}
	if overloaded {
		notes = append(notes, "留果偏多会拖慢糖度积累，适度疏果可改善粒重与成熟。")
	}
	if state.Stats.SpoiledWeight > 0 {
		notes = append(notes, "采后存放造成损耗；及时销售或建设冷库可减少损失。")
	}
	if len(state.Inventory) > 0 {
		notes = append(notes, "库存仍可继续销售；售空后才能开启下一季。")
	}
	if state.TotalCosts > state.TotalRevenue {
		notes = append(notes, "本季支出超过销售收入；设施建设费用计入当季，可在后续季节继续使用。")
	}
	if len(notes) == 0 {
		notes = append(notes, "完成了从田间管理到采收销售的一季经营，下一季可继续优化成本与品质。")
	}
	if harvested > 0 {
		brix = vineyardRound(brix / float64(harvested))
	}
	return &vineyardReport{state.Season, state.Stats.HarvestWeight, state.Stats.SoldWeight, state.Stats.SpoiledWeight, state.TotalRevenue, state.TotalCosts, state.TotalRevenue - state.TotalCosts, brix, bestGrade, notes}
}
func vineyardFacilityEnabled(f vineyardFacilities, key string) bool {
	switch key {
	case "drip":
		return f.Drip
	case "shelter":
		return f.Shelter
	case "coldStorage":
		return f.ColdStorage
	}
	return false
}
func setVineyardFacility(f *vineyardFacilities, key string) {
	switch key {
	case "drip":
		f.Drip = true
	case "shelter":
		f.Shelter = true
	case "coldStorage":
		f.ColdStorage = true
	}
}
func vineyardLastAction(actions *vineyardLastActions, key string) *int {
	switch key {
	case "water":
		return &actions.Water
	case "fertilize":
		return &actions.Fertilize
	case "prune":
		return &actions.Prune
	case "thin":
		return &actions.Thin
	case "protect":
		return &actions.Protect
	}
	return nil
}
func spendVineyardCoins(state *vineyardFarm, cost int) error {
	if state.Coins < cost {
		return gameError("INSUFFICIENT_COINS", "经营币不足，请先销售库存或调整计划")
	}
	state.Coins -= cost
	state.TotalCosts += cost
	return nil
}

func simulateVineyardDay(state *vineyardFarm) {
	state.Day++
	weather := getVineyardWeather(state.Seed^(uint32(state.Season-1)*uint32(0x85EBCA6B)), state.Day)
	stage := getVineyardStage(state.Day)
	upkeep := 0
	for _, key := range []string{"drip", "shelter", "coldStorage"} {
		if vineyardFacilityEnabled(state.Facilities, key) && (key != "coldStorage" || len(state.Inventory) > 0) {
			upkeep += vineyardFacilityRules[key].DailyCost
		}
	}
	maintained := upkeep == 0 || state.Coins >= upkeep
	if maintained && upkeep > 0 {
		_ = spendVineyardCoins(state, upkeep)
		appendVineyardHistory(state, "upkeep", "设施日常维护", vineyardIntPointer(upkeep), nil)
	}
	if !maintained {
		appendVineyardHistory(state, "warning", "经营币不足，今日设施未运行", nil, nil)
	}
	drip, shelter := state.Facilities.Drip && maintained, state.Facilities.Shelter && maintained
	for index := range state.Plots {
		plot := &state.Plots[index]
		beforeMoisture := plot.Moisture
		rain := weather.Rain
		if shelter {
			rain *= 0.2
		}
		evap := float64(7)
		if weather.Kind == "hot" {
			evap = 10
		}
		afterWeather := plot.Moisture + rain - evap
		irrigation := float64(0)
		if drip {
			irrigation = vineyardClamp(62-afterWeather, 0, 7)
		}
		plot.Moisture = vineyardRound(vineyardClamp(afterWeather+irrigation, 0, 100))
		fertilityUse := 0.24
		if state.Day >= 21 && state.Day <= 55 {
			fertilityUse = 0.62
		}
		plot.Fertility = vineyardRound(vineyardClamp(plot.Fertility-fertilityUse, 0, 100))
		dry := vineyardClamp((28-plot.Moisture)/28, 0, 1)
		wet := vineyardClamp((plot.Moisture-88)/12, 0, 1)
		excess := vineyardClamp((plot.Fertility-78)/22, 0, 1)
		nutrient := vineyardClamp(plot.Fertility/42, 0.4, 1)
		canopyFactor := vineyardClamp(1-math.Abs(plot.Canopy-62)/100, 0.35, 1)
		overload := vineyardClamp((plot.Load-plot.Vigor*0.95)/40, 0, 1)
		waterFactor := 1 - dry*0.72 - wet*0.24
		health := vineyardClamp(1-plot.Damage/140, 0.35, 1)
		if !plot.Harvested && state.Day < 84 {
			if state.Day >= 7 && state.Day <= 55 {
				plot.Canopy = vineyardRound(vineyardClamp(plot.Canopy+0.32+excess*0.65, 0, 100))
			}
			if state.Day >= 21 {
				infection := 0.08
				if weather.Humidity >= 75 {
					infection = 0.8 + math.Max(0, plot.Canopy-72)*0.035
				}
				if shelter {
					infection *= 0.52
				}
				if state.Day-plot.LastActions.Protect <= 7 {
					infection *= 0.28
				}
				cracking, sunburn := float64(0), float64(0)
				if state.Day >= 56 && beforeMoisture < 32 && rain >= 8 {
					cracking = 2.8
				}
				if weather.Kind == "hot" && plot.Canopy < 42 {
					sunburn = (42 - plot.Canopy) * 0.07
				}
				plot.Damage = vineyardRound(vineyardClamp(plot.Damage+infection+cracking+sunburn, 0, 100))
			}
			growth := float64(0)
			if state.Day >= 35 && state.Day <= 55 {
				growth = 0.26
			} else if state.Day >= 21 && state.Day <= 34 {
				growth = 0.07
			} else if state.Day >= 56 {
				growth = 0.045
			}
			plot.BerryWeight = vineyardRound(vineyardClamp(plot.BerryWeight+growth*waterFactor*nutrient*canopyFactor*(1-overload*0.5)*health, 0, 12))
			if state.Day >= 56 {
				shadeFactor := float64(1)
				if shelter {
					shadeFactor = 0.97
				}
				sugar := 0.72 * waterFactor * canopyFactor * health * (1 - overload*0.42) * (1 - excess*0.24) * shadeFactor
				plot.Brix = vineyardRound(vineyardClamp(plot.Brix+sugar, 0, 24))
			}
			recovery := float64(0)
			if dry == 0 && nutrient >= 0.9 {
				recovery = 0.045
			}
			plot.Vigor = vineyardRound(vineyardClamp(plot.Vigor-dry*0.42-overload*0.12-excess*0.14+recovery, 0, 100))
		} else if plot.Harvested {
			recovery := -0.12
			if dry == 0 {
				recovery = 0.18
			}
			plot.Vigor = vineyardRound(vineyardClamp(plot.Vigor+recovery, 0, 100))
		}
	}
	cold := state.Facilities.ColdStorage && maintained
	spoiled := 0
	kept := []vineyardBatch{}
	for _, batch := range state.Inventory {
		rate := 0.04
		if cold {
			rate = 0.008
		} else if weather.Kind == "hot" {
			rate = 0.065
		}
		loss := min(batch.Weight, int(math.Ceil(float64(batch.Weight)*rate)))
		batch.Weight -= loss
		spoiled += loss
		if batch.Weight > 0 {
			kept = append(kept, batch)
		}
	}
	state.Inventory = kept
	state.Stats.SpoiledWeight += spoiled
	if spoiled > 0 {
		appendVineyardHistory(state, "spoilage", fmt.Sprintf("采后损耗 %d 克，库存已同步减少", spoiled), nil, nil)
	}
	for _, item := range vineyardStages {
		if item.Start == state.Day {
			appendVineyardHistory(state, "stage", "进入"+stage.Name+"阶段", nil, nil)
			break
		}
	}
	if state.Day == 84 {
		state.Report = getVineyardReport(*state)
		appendVineyardHistory(state, "seasonEnd", "本季生长结束，剩余库存可继续销售", nil, nil)
	}
}

func settleVineyardFarm(state vineyardFarm, now int64) (vineyardFarm, error) {
	if !validateVineyardSave(state) {
		return state, gameError("INVALID_SAVE", "农场存档无效，请恢复有效存档")
	}
	if now < 0 || now > vineyardSafeInteger || now < state.LastSettledAt {
		return state, gameError("INVALID_TIME", "结算时间无效")
	}
	next := cloneVineyardFarm(state)
	days := min(84-next.Day, int((now-next.LastSettledAt)/vineyardOfflineDayMS))
	for index := 0; index < days; index++ {
		simulateVineyardDay(&next)
	}
	if days > 0 {
		next.LastSettledAt += int64(days) * vineyardOfflineDayMS
		next.Version++
	}
	return next, nil
}
func validateVineyardCommand(command vineyardCommand) error {
	_, plotAction := vineyardActionRules[command.Type]
	if plotAction {
		if !vineyardInt(command.PlotID, 1, 4) {
			return gameError("INVALID_PLOT", "请选择有效地块")
		}
		if command.Channel != "" || command.Facility != "" {
			return gameError("INVALID_REQUEST", "农事参数无效")
		}
		return nil
	}
	if command.PlotID != 0 {
		return gameError("INVALID_REQUEST", "农场操作参数无效")
	}
	switch command.Type {
	case "advance", "newSeason":
		if command.Channel != "" || command.Facility != "" {
			return gameError("INVALID_REQUEST", "农场操作参数无效")
		}
	case "sell":
		if command.Channel != "wholesale" && command.Channel != "retail" {
			return gameError("INVALID_CHANNEL", "请选择批发或零售渠道")
		}
		if command.Facility != "" {
			return gameError("INVALID_REQUEST", "销售参数无效")
		}
	case "upgrade":
		if _, ok := vineyardFacilityRules[command.Facility]; !ok {
			return gameError("INVALID_FACILITY", "设施类型无效")
		}
		if command.Channel != "" {
			return gameError("INVALID_REQUEST", "设施参数无效")
		}
	default:
		return gameError("UNKNOWN_ACTION", "不支持的农场操作")
	}
	return nil
}
func applyVineyardAction(state vineyardFarm, command vineyardCommand, now int64) (vineyardFarm, error) {
	if err := validateVineyardCommand(command); err != nil {
		return state, err
	}
	next, err := settleVineyardFarm(state, now)
	if err != nil {
		return state, err
	}
	switch command.Type {
	case "advance":
		if next.Day == 84 {
			return state, gameError("SEASON_CLOSED", "本季已结束，请先销售库存再开启下一季")
		}
		days := min(7, 84-next.Day)
		for index := 0; index < days; index++ {
			simulateVineyardDay(&next)
		}
		next.LastSettledAt = now
		appendVineyardHistory(&next, "advance", fmt.Sprintf("推进 %d 天，水分、果实与库存已结算", days), nil, nil)
	case "newSeason":
		if next.Day != 84 {
			return state, gameError("SEASON_NOT_OVER", "请先完成本季经营")
		}
		if len(next.Inventory) > 0 {
			return state, gameError("UNSOLD_INVENTORY", "还有葡萄库存，请先销售后开启下一季")
		}
		if next.Season >= 10000 {
			return state, gameError("LIMIT_REACHED", "已达到存档季数上限")
		}
		next.Season++
		next.Day = 0
		next.LastSettledAt = now
		for index, plot := range next.Plots {
			next.Plots[index] = makeVineyardPlot(plot.ID, vineyardClamp(plot.Vigor+5, 45, 95))
		}
		next.TotalCosts = 0
		next.TotalRevenue = 0
		next.Report = nil
		next.Stats = vineyardStats{}
		appendVineyardHistory(&next, "newSeason", "开启下一季，保留经营币、设施与多年生树势", nil, nil)
	case "sell":
		if len(next.Inventory) == 0 {
			return state, gameError("NO_INVENTORY", "暂无可销售的葡萄库存")
		}
		prices := map[string]int{"A": 66, "B": 43, "C": 23}
		channelCost, channelName := 2, "批发"
		if command.Channel == "retail" {
			prices = map[string]int{"A": 91, "B": 60, "C": 32}
			channelCost, channelName = 7, "零售"
		}
		revenue, cost, weight := 0, 0, 0
		for _, batch := range next.Inventory {
			revenue += batch.Weight * prices[batch.Grade] / 1000
			cost += int(math.Ceil(float64(batch.Weight*channelCost) / 1000))
			weight += batch.Weight
		}
		if next.Coins+revenue < cost {
			return state, gameError("INSUFFICIENT_COINS", "无法支付销售渠道费用")
		}
		next.Coins += revenue - cost
		next.TotalRevenue += revenue
		next.TotalCosts += cost
		next.Stats.SoldWeight += weight
		next.Inventory = []vineyardBatch{}
		appendVineyardHistory(&next, "sell", fmt.Sprintf("%s售出 %d 克葡萄", channelName, weight), vineyardIntPointer(cost), vineyardIntPointer(revenue))
	case "upgrade":
		if next.Day == 84 {
			return state, gameError("SEASON_CLOSED", "请下一季再建设设施")
		}
		if vineyardFacilityEnabled(next.Facilities, command.Facility) {
			return state, gameError("ALREADY_UPGRADED", "这项设施已经建成")
		}
		facility := vineyardFacilityRules[command.Facility]
		if err := spendVineyardCoins(&next, facility.Cost); err != nil {
			return state, err
		}
		setVineyardFacility(&next.Facilities, command.Facility)
		appendVineyardHistory(&next, "upgrade", "建设"+facility.Name+"，运行需要日常维护费用", vineyardIntPointer(facility.Cost), nil)
	default:
		var plot *vineyardPlot
		for index := range next.Plots {
			if next.Plots[index].ID == command.PlotID {
				plot = &next.Plots[index]
				break
			}
		}
		rule := vineyardActionRules[command.Type]
		if next.Day < rule.Start || next.Day > rule.End {
			return state, gameError("WRONG_STAGE", rule.Name+"不在当前农事窗口")
		}
		if plot.Harvested {
			return state, gameError("ALREADY_HARVESTED", "此地块已采收，下一季继续管理")
		}
		last := vineyardLastAction(&plot.LastActions, command.Type)
		if command.Type != "harvest" && next.Day-*last < rule.Cooldown {
			return state, gameError("ACTION_COOLDOWN", "这项农事刚完成，请等待下一个管理窗口")
		}
		if command.Type == "harvest" && plot.Brix < 15 {
			return state, gameError("NOT_RIPE", "糖度未到 15，继续成熟后采收")
		}
		if command.Type == "thin" && plot.Load <= 24 {
			return state, gameError("TOO_FEW_FRUITS", "留果量已经很低，不宜继续疏果")
		}
		if command.Type == "prune" && plot.Canopy <= 24 {
			return state, gameError("TOO_FEW_LEAVES", "叶量已经很少，不宜继续整枝")
		}
		if err := spendVineyardCoins(&next, rule.Cost); err != nil {
			return state, err
		}
		text := plot.Name + "完成" + rule.Name
		switch command.Type {
		case "water":
			plot.Moisture = vineyardRound(vineyardClamp(plot.Moisture+34, 0, 100))
			text += "，补充水分；持续过湿会影响生长"
		case "fertilize":
			plot.Fertility = vineyardRound(vineyardClamp(plot.Fertility+27, 0, 100))
			text += "，养分补充；过量会使枝叶旺长、成熟变慢"
		case "prune":
			plot.Canopy = vineyardRound(vineyardClamp(plot.Canopy-18, 0, 100))
			text += "，改善通风；叶量过低会影响糖度和日灼风险"
		case "thin":
			plot.Load = vineyardRound(vineyardClamp(plot.Load-20, 0, 100))
			text += "，减轻负载；总果数随之下降"
		case "protect":
			text += "，未来 7 天降低病害新增风险，已有损伤不会消失"
		case "harvest":
			weight := max(1, int(math.Floor(9500*plot.Load/100*plot.BerryWeight*(1-plot.Damage/100))))
			grade := gradeVineyardPlot(*plot)
			next.Inventory = append(next.Inventory, vineyardBatch{fmt.Sprintf("s%d-p%d", next.Season, plot.ID), plot.ID, grade, weight, weight, plot.Brix, next.Day, next.Season})
			next.Stats.HarvestWeight += weight
			plot.Harvested = true
			text += fmt.Sprintf("，%s 级 %d 克，糖度 %s", grade, weight, strconv.FormatFloat(plot.Brix, 'f', -1, 64))
		}
		if command.Type != "harvest" {
			*last = next.Day
		}
		appendVineyardHistory(&next, command.Type, text, vineyardIntPointer(rule.Cost), nil)
	}
	if next.Day == 84 {
		next.Report = getVineyardReport(next)
	}
	next.Version++
	if !validateVineyardSave(next) {
		return state, gameError("INVALID_SAVE", "农场存档无效，请恢复有效存档")
	}
	return next, nil
}
