package main

var themeList = []struct {
	name        string
	displayName string
	theme       Theme
}{
	{"halo", "Halo", Halo},
	{"cornflower", "Cornflower", Cornflower},
	{"whatsapp", "WhatsApp", WhatsApp},
	{"linen", "Linen", Linen},
	{"tokyonight", "Tokyo Night", TokyoNight},
	{"catppuccin", "Catppuccin", Catppuccin},
	{"monokai", "Monokai", Monokai},
	{"charcoal", "Charcoal", Charcoal},
	{"aurora", "Aurora", Aurora},
	{"sakura", "Sakura", Sakura},
	{"abyssal", "Abyssal", Abyssal},
	{"ember", "Ember", Ember},
	{"glacier", "Glacier", Glacier},
	{"verdant", "Verdant", Verdant},
	{"dusk", "Dusk", Dusk},
	{"fossil", "Fossil", Fossil},
}

func applyThemeByName(name string) {
	for _, t := range themeList {
		if t.name == name {
			currentTheme = t.theme
			currentConfig.ThemeName = name
			rehashStyles()
			return
		}
	}
	currentTheme = themeList[0].theme
	currentConfig.ThemeName = themeList[0].name
	rehashStyles()
}

func buildThemePickerItems() []pickerItem {
	items := make([]pickerItem, len(themeList))
	for i, t := range themeList {
		items[i] = pickerItem{key: t.name, label: t.displayName}
	}
	return items
}
