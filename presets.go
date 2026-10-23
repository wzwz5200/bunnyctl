package main

// Built-in presets and the factory tuning recovered from the dongle's flash
// (config block at flash offset 0x9000, mirrored at 0xA000).

// Preset is a named set of EQ bands.
type Preset struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Desc  string `json:"desc"`
	Bands []Band `json:"bands"`
}

var factoryDAC = EQ{
	Bank:    "dac",
	Enabled: true,
	Bands: []Band{
		{Freq: 30, Gain: -4.0, Q: 0.300, Type: "PK"},
		{Freq: 200, Gain: -6.0, Q: 0.500, Type: "PK"},
		{Freq: 800, Gain: -0.3, Q: 0.500, Type: "PK"},
		{Freq: 1700, Gain: -4.0, Q: 0.600, Type: "PK"},
		{Freq: 7500, Gain: -6.0, Q: 0.900, Type: "PK"},
	},
}

var factoryADC = EQ{
	Bank:    "adc",
	Enabled: false,
	Bands: []Band{
		{Freq: 1000, Gain: 0, Q: 0.707, Type: "PK"},
		{Freq: 2000, Gain: 0, Q: 0.707, Type: "PK"},
		{Freq: 5000, Gain: 0, Q: 0.707, Type: "PK"},
		{Freq: 8000, Gain: 0, Q: 0.707, Type: "PK"},
		{Freq: 10000, Gain: 0, Q: 0.707, Type: "PK"},
	},
}

var presets = []Preset{
	{
		ID: "flat", Name: "平直", Desc: "全部归零，听单元本色",
		Bands: []Band{
			{Freq: 60, Gain: 0, Q: 0.7, Type: "PK"},
			{Freq: 250, Gain: 0, Q: 0.7, Type: "PK"},
			{Freq: 1000, Gain: 0, Q: 0.7, Type: "PK"},
			{Freq: 3000, Gain: 0, Q: 0.7, Type: "PK"},
			{Freq: 8000, Gain: 0, Q: 0.7, Type: "PK"},
		},
	},
	{
		ID: "factory", Name: "原厂", Desc: "天使吉米出厂调音（已从固件读出）",
		Bands: factoryDAC.Bands,
	},
	{
		ID: "bass", Name: "低频增强", Desc: "下潜 + 一点齿音抑制",
		Bands: []Band{
			{Freq: 45, Gain: 5.0, Q: 0.55, Type: "LSH"},
			{Freq: 180, Gain: 2.0, Q: 0.80, Type: "PK"},
			{Freq: 900, Gain: -1.0, Q: 0.70, Type: "PK"},
			{Freq: 3200, Gain: 0.5, Q: 0.90, Type: "PK"},
			{Freq: 6800, Gain: -2.0, Q: 1.00, Type: "PK"},
		},
	},
	{
		ID: "vocal", Name: "人声", Desc: "贴耳、提中频、压齿音",
		Bands: []Band{
			{Freq: 250, Gain: -2.5, Q: 0.70, Type: "PK"},
			{Freq: 1100, Gain: 1.5, Q: 0.80, Type: "PK"},
			{Freq: 2600, Gain: 2.5, Q: 1.00, Type: "PK"},
			{Freq: 5500, Gain: -2.5, Q: 1.20, Type: "PK"},
			{Freq: 9000, Gain: -1.0, Q: 0.80, Type: "PK"},
		},
	},
	{
		ID: "smooth", Name: "耐听", Desc: "高切齿音、削 6k 峰",
		Bands: []Band{
			{Freq: 80, Gain: 1.0, Q: 0.70, Type: "LSH"},
			{Freq: 400, Gain: 1.0, Q: 0.80, Type: "PK"},
			{Freq: 1500, Gain: 0.5, Q: 1.00, Type: "PK"},
			{Freq: 6000, Gain: -4.0, Q: 1.20, Type: "PK"},
			{Freq: 10000, Gain: -2.0, Q: 0.90, Type: "HSH"},
		},
	},
	{
		ID: "harman", Name: "哈曼近似", Desc: "低频搁架 + 中高频微调",
		Bands: []Band{
			{Freq: 105, Gain: 5.5, Q: 0.70, Type: "LSH"},
			{Freq: 1000, Gain: 0.0, Q: 0.70, Type: "PK"},
			{Freq: 3000, Gain: -1.5, Q: 1.00, Type: "PK"},
			{Freq: 6000, Gain: -2.0, Q: 1.50, Type: "PK"},
			{Freq: 10000, Gain: 1.5, Q: 0.70, Type: "HSH"},
		},
	},
}
