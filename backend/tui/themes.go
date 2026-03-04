package main

var (
	// TokyoNight — deep navy #1a1b26 bg, cool blue-purple chrome, warm green/cyan accents
	TokyoNight = Theme{
		Brand:                 "#9ece6a", // official Tokyo Night green
		Accent:                "#7dcfff", // official Tokyo Night cyan
		Purple:                "#bb9af7", // official Tokyo Night purple
		Amber:                 "#e0af68", // official Tokyo Night orange-gold
		Red:                   "#f7768e", // official Tokyo Night red
		Muted:                 "#565f89", // blue-gray comment color
		Text:                  "#c0caf5", // blue-white foreground
		ImageTag:              "#f7768e", // red
		VideoTag:              "#7dcfff", // cyan
		AudioTag:              "#fbbf24", // warm yellow — distinct from amber
		FileTag:               "#9ece6a", // green
		StickerTag:            "#bb9af7", // purple
		ContactTag:            "#73daca", // teal
		PollTag:               "#e0af68", // amber-gold
		LocationTag:           "#7aa2f7", // blue
		AnomalyTag:            "#f7768e", // red
		SentText:              "#c3e88d", // soft lime-green — ties sent to brand
		ReceivedText:          "#bab0f0", // soft periwinkle — incoming, purple family
		SentName:              "#9ece6a", // brand green — "me" in groups
		ReceivedName:          "#bb9af7", // vivid purple — "them", bold hierarchy
		QuotedSentText:        "#8ca38b", // 50% blend sentText+muted — ghost green for quoted own
		QuotedReceivedText:    "#8887bc", // 50% blend receivedText+muted — ghost purple for quoted theirs
		BadgeInk:              "#1a1b26",
		ButtonInk:             "#1a1b26",
		TagInk:                "#1a1b26",
		Cursor:                "#c0caf5", // text-color cursor — less harsh than white
		QRLight:               "#FFFFFF",
		QRDark:                "#000000",
		ShortcutActive:        "#24283b", // Tokyo Night surface bg
		SidebarActiveBg:       "#2e3566", // rich navy-blue tint — cool, on-theme
		SidebarActiveUnreadBg: "#252a40", // cool navy for unread — no amber bleed
		ReplyPreviewBg:        "#1d2545", // deep navy reply context
		MessageSelectedBg:     "#232845", // subtle navy highlight
		MediaTokenBg:          "#f7768e", // theme's own red — more cohesive
		MediaTokenPulseBg:     "#ff9eb5", // soft red-pink pulse
	}

	// Catppuccin Mocha — official Catppuccin Mocha palette, #1e1e2e bg
	Catppuccin = Theme{
		Brand:                 "#a6e3a1", // Mocha Green (official)
		Accent:                "#89b4fa", // Mocha Blue (official)
		Purple:                "#cba4f7", // Mocha Mauve (official)
		Amber:                 "#f9e2af", // Mocha Yellow (official)
		Red:                   "#f38ba8", // Mocha Red (official)
		Muted:                 "#6c7086", // Mocha Overlay0 (official)
		Text:                  "#cdd6f4", // Mocha Text (official)
		ImageTag:              "#f38ba8", // Mocha Red
		VideoTag:              "#89b4fa", // Mocha Blue
		AudioTag:              "#fab387", // Mocha Peach
		FileTag:               "#a6e3a1", // Mocha Green
		StickerTag:            "#cba4f7", // Mocha Mauve
		ContactTag:            "#94e2d5", // Mocha Teal
		PollTag:               "#f9e2af", // Mocha Yellow
		LocationTag:           "#89dceb", // Mocha Sky
		AnomalyTag:            "#f38ba8", // Mocha Red
		SentText:              "#d0efc5", // soft sage-mint — brand green family
		ReceivedText:          "#dcd0f5", // soft mauve-lavender — incoming, purple family
		SentName:              "#a6e3a1", // brand green — "me" label
		ReceivedName:          "#cba4f7", // vivid mauve — "them" label
		QuotedSentText:        "#9eafa5", // 50% blend sentText+muted — ghost sage for quoted own
		QuotedReceivedText:    "#a4a0bd", // 50% blend receivedText+muted — ghost mauve for quoted theirs
		BadgeInk:              "#1e1e2e",
		ButtonInk:             "#1e1e2e",
		TagInk:                "#1e1e2e",
		Cursor:                "#cdd6f4", // text-color cursor
		QRLight:               "#FFFFFF",
		QRDark:                "#000000",
		ShortcutActive:        "#2a2a3d", // Mocha Surface0 variant
		SidebarActiveBg:       "#313244", // Mocha Surface0 (official)
		SidebarActiveUnreadBg: "#2a2545", // mauve-tinted navy — on-theme for unread
		ReplyPreviewBg:        "#272040", // dark mauve context
		MessageSelectedBg:     "#252040", // deep mauve selection
		MediaTokenBg:          "#f38ba8", // Mocha Red
		MediaTokenPulseBg:     "#f5bde6", // Mocha Pink — soft pulse
	}

	// Monokai — classic #272822 bg, vivid 6-color palette
	Monokai = Theme{
		Brand:                 "#a6e22e", // Monokai green
		Accent:                "#66d9ef", // Monokai cyan
		Purple:                "#ae81ff", // Monokai purple
		Amber:                 "#e6db74", // Monokai yellow
		Red:                   "#f92672", // Monokai red
		Muted:                 "#75715e", // Monokai comment
		Text:                  "#f8f8f2", // Monokai foreground
		ImageTag:              "#f92672", // red
		VideoTag:              "#66d9ef", // cyan
		AudioTag:              "#fd971f", // Monokai orange — distinct from yellow PollTag
		FileTag:               "#a6e22e", // green
		StickerTag:            "#ae81ff", // purple
		ContactTag:            "#78dce8", // Monokai Pro teal — distinct from cyan
		PollTag:               "#e6db74", // yellow
		LocationTag:           "#66d9ef", // cyan (exact official)
		AnomalyTag:            "#f92672", // red — using official red, not external pink
		SentText:              "#d9f7a7", // warm lime — outgoing, brand green family
		ReceivedText:          "#c0eeff", // soft cyan-white — incoming, accent family
		SentName:              "#a6e22e", // brand green — "me" label
		ReceivedName:          "#ae81ff", // purple — "them", clearly distinct
		QuotedSentText:        "#a7b482", // 50% blend sentText+muted — ghost olive for quoted own
		QuotedReceivedText:    "#9aafae", // 50% blend receivedText+muted — ghost cyan for quoted theirs
		BadgeInk:              "#1f1f1f",
		ButtonInk:             "#1f1f1f",
		TagInk:                "#1f1f1f",
		Cursor:                "#f8f8f2", // foreground color cursor
		QRLight:               "#FFFFFF",
		QRDark:                "#000000",
		ShortcutActive:        "#2a2820", // darker than Monokai bg
		SidebarActiveBg:       "#3e3d32", // Monokai bg lightened
		SidebarActiveUnreadBg: "#3e3520", // warm yellow-green tint — on-theme
		ReplyPreviewBg:        "#332c18", // dark warm Monokai
		MessageSelectedBg:     "#2d2910", // very dark warm highlight
		MediaTokenBg:          "#f92672", // Monokai red
		MediaTokenPulseBg:     "#ff6b9d", // soft pink pulse
	}

	// Charcoal — pure monochrome, dark #1c1c1c bg, deliberate luminance steps
	Charcoal = Theme{
		Brand:                 "#ebebeb", // near-white brand
		Accent:                "#d0d0d0", // lighter gray accent
		Purple:                "#b8b8b8", // mid-gray "purple"
		Amber:                 "#e0e0e0", // lighter gray "amber"
		Red:                   "#a0a0a0", // darker gray "red"
		Muted:                 "#686868", // clearly muted
		Text:                  "#f0f0f0", // off-white text — easier on eyes
		ImageTag:              "#e2e2e2", // brightest tag tier
		VideoTag:              "#cccccc", // second tier
		AudioTag:              "#b8b8b8", // third tier
		FileTag:               "#d8d8d8", // bright-medium
		StickerTag:            "#a8a8a8", // lower mid
		ContactTag:            "#c0c0c0", // medium
		PollTag:               "#d0d0d0", // upper mid
		LocationTag:           "#989898", // darker tier
		AnomalyTag:            "#808080", // darkest — visually distinct as anomaly
		SentText:              "#f0f0f0", // near-white — my messages pop
		ReceivedText:          "#bebebe", // clearly dimmer — their messages
		SentName:              "#ebebeb", // brightest gray for "me"
		ReceivedName:          "#adadad", // distinctly subordinate
		QuotedSentText:        "#acacac", // 50% blend sentText+muted — dimmer gray for quoted own
		QuotedReceivedText:    "#939393", // 50% blend receivedText+muted — darker gray for quoted theirs
		BadgeInk:              "#1a1a1a",
		ButtonInk:             "#1a1a1a",
		TagInk:                "#1a1a1a",
		Cursor:                "#f0f0f0", // text-color cursor
		QRLight:               "#FFFFFF",
		QRDark:                "#000000",
		ShortcutActive:        "#2e2e2e",
		SidebarActiveBg:       "#2d2d2d",
		SidebarActiveUnreadBg: "#333333", // neutral gray, no warm tint
		ReplyPreviewBg:        "#3f3f3f",
		MessageSelectedBg:     "#383838",
		MediaTokenBg:          "#c8c8c8",
		MediaTokenPulseBg:     "#e0e0e0",
	}

	// Aurora — Northern Lights on deep night sky, vivid electric palette across the spectrum
	Aurora = Theme{
		Brand:                 "#50fa7b", // electric green — aurora's primary band
		Accent:                "#8be9fd", // electric cyan — aurora blue
		Purple:                "#bd93f9", // aurora violet/amethyst
		Amber:                 "#ffb86c", // aurora warm orange glow
		Red:                   "#ff5555", // aurora red fringe
		Muted:                 "#44475a", // deep night sky muted
		Text:                  "#f8f8f2", // star-white foreground
		ImageTag:              "#ff79c6", // aurora pink/magenta
		VideoTag:              "#8be9fd", // electric cyan
		AudioTag:              "#f1fa8c", // aurora yellow-green flash
		FileTag:               "#50fa7b", // electric green
		StickerTag:            "#bd93f9", // violet
		ContactTag:            "#6dcfb4", // teal — distinct from cyan and green
		PollTag:               "#ffb86c", // warm orange
		LocationTag:           "#80bfff", // sky blue
		AnomalyTag:            "#ff5555", // aurora red
		SentText:              "#ccffdc", // soft mint — outgoing, green family
		ReceivedText:          "#e0d8ff", // soft violet — incoming, DISTINCT hue from sent
		SentName:              "#50fa7b", // brand electric green — "me"
		ReceivedName:          "#bd93f9", // violet — "them", clearly distinct from green
		QuotedSentText:        "#88a39b", // 50% blend sentText+muted — ghost mint for quoted own
		QuotedReceivedText:    "#928fac", // 50% blend receivedText+muted — ghost violet for quoted theirs
		BadgeInk:              "#0d1117",
		ButtonInk:             "#0d1117",
		TagInk:                "#0d1117",
		Cursor:                "#50fa7b", // brand green cursor — glowing
		QRLight:               "#FFFFFF",
		QRDark:                "#0d1117",
		ShortcutActive:        "#1a1e2e",
		SidebarActiveBg:       "#1e2540", // deep aurora night sky blue
		SidebarActiveUnreadBg: "#18272a", // dark teal — aurora green band
		ReplyPreviewBg:        "#1a2030",
		MessageSelectedBg:     "#14192a", // very dark navy
		MediaTokenBg:          "#ff79c6", // aurora pink
		MediaTokenPulseBg:     "#ffb3e0", // soft pink pulse
	}

	// Sakura — Cherry blossom, dark plum bg, pink/rose/lavender palette
	Sakura = Theme{
		Brand:                 "#ffb7d5", // sakura petal — soft light pink
		Accent:                "#f472b6", // vibrant cherry — deeper pink
		Purple:                "#e879f9", // wisteria/orchid
		Amber:                 "#fcd34d", // golden stamen — warm contrast
		Red:                   "#fb7185", // coral rose
		Muted:                 "#8b6070", // dusty rose-gray
		Text:                  "#fce8f3", // warm blush white
		ImageTag:              "#f472b6", // cherry pink
		VideoTag:              "#c084fc", // soft purple — cool contrast to pink
		AudioTag:              "#fcd34d", // golden stamen
		FileTag:               "#a5f3fc", // sakura sky blue — cherry blossom viewing sky
		StickerTag:            "#e879f9", // orchid/wisteria
		ContactTag:            "#fda4af", // soft rose
		PollTag:               "#fde68a", // soft gold
		LocationTag:           "#93c5fd", // spring sky blue
		AnomalyTag:            "#fb7185", // coral
		SentText:              "#ffe8f5", // warm rose-white — soft, "mine"
		ReceivedText:          "#f0e0ff", // soft lavender — "theirs", distinct hue
		SentName:              "#ffb7d5", // brand light-pink — "me" label
		ReceivedName:          "#e879f9", // vivid orchid — "them", pops against pink
		QuotedSentText:        "#c5a4b2", // 50% blend sentText+muted — ghost rose for quoted own
		QuotedReceivedText:    "#bda0b7", // 50% blend receivedText+muted — ghost lavender for quoted theirs
		BadgeInk:              "#1a0d14",
		ButtonInk:             "#1a0d14",
		TagInk:                "#1a0d14",
		Cursor:                "#ffb7d5", // brand pink cursor — thematic
		QRLight:               "#FFFFFF",
		QRDark:                "#1a0d14",
		ShortcutActive:        "#231019",
		SidebarActiveBg:       "#3d1f35",  // rich plum — active
		SidebarActiveUnreadBg: "#2a1020",  // clearly darker plum — unread, distinct from active
		ReplyPreviewBg:        "#4a1d3f",  // deep plum reply context
		MessageSelectedBg:     "#351430",  // deep burgundy selection
		MediaTokenBg:          "#db2777",
		MediaTokenPulseBg:     "#f472b6",
	}

	// Abyssal — deep ocean bioluminescence, near-black #020810 bg
	Abyssal = Theme{
		Brand:                 "#00e5c8", // bioluminescent teal — signature glow
		Accent:                "#00cfff", // electric cyan
		Purple:                "#7b6cff", // deep ocean violet
		Amber:                 "#ffb347", // anglerfish amber lure
		Red:                   "#ff4f6e", // bioluminescent red
		Muted:                 "#2d4a5a", // deep ocean blue-gray
		Text:                  "#c4dce8", // cool ocean-light text
		ImageTag:              "#ff79b0", // pink bioluminescence — vivid, unique
		VideoTag:              "#00cfff", // electric cyan
		AudioTag:              "#ffb347", // anglerfish amber
		FileTag:               "#00e5c8", // teal bioluminescence
		StickerTag:            "#7b6cff", // deep violet
		ContactTag:            "#40e0d0", // turquoise — distinct from teal and cyan above
		PollTag:               "#ffd166", // golden yellow — distinct from amber
		LocationTag:           "#74b9ff", // deep ocean blue — distinct from cyan
		AnomalyTag:            "#ff4f6e", // bioluminescent red
		SentText:              "#a8f0e8", // bioluminescent teal-tinted — outgoing
		ReceivedText:          "#b8d0ff", // soft electric blue — incoming, distinct hue
		SentName:              "#00e5c8", // brand teal — "me", glowing
		ReceivedName:          "#7b6cff", // deep violet — "them", clearly distinct
		QuotedSentText:        "#6a9da1", // 50% blend sentText+muted — ghost teal for quoted own
		QuotedReceivedText:    "#728dac", // 50% blend receivedText+muted — ghost blue for quoted theirs
		BadgeInk:              "#020c14",
		ButtonInk:             "#020c14",
		TagInk:                "#020c14",
		Cursor:                "#00e5c8", // glowing teal cursor
		QRLight:               "#FFFFFF",
		QRDark:                "#020c14",
		ShortcutActive:        "#0d1f2d",
		SidebarActiveBg:       "#0a2233",
		SidebarActiveUnreadBg: "#102a20", // dark teal-green — distinct from blue active
		ReplyPreviewBg:        "#0c2030",
		MessageSelectedBg:     "#091c28",
		MediaTokenBg:          "#0077aa",
		MediaTokenPulseBg:     "#00cfff",
	}
)
