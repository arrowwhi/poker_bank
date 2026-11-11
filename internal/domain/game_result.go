package domain

// GameResult holds the financial outcome for a player in a finished game.
type GameResult struct {
	GameID        int64
	PlayerTgID    int64
	BuyInCount    int
	RebuyCount    int
	TotalInRub    int
	TotalOutRub   int
	TotalOutChips int
	NetRub        int
}

// PlayerChatStats aggregates a player's performance across all games in a chat.
type PlayerChatStats struct {
	PlayerTgID  int64
	GameCount   int
	TotalNetRub int
	WinsCount   int
	// Поля ниже заполняет только LeaderboardSince (рейтинг в Mini App).
	// TotalInRub — Σ внесённого (buy-in + rebuy), знаменатель для «% от входа».
	TotalInRub int
	// TotalNetChips — Σ (фишки на выходе − фишки на входе).
	TotalNetChips int
}
