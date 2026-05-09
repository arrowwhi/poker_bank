package domain

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

type PlayerChatStats struct {
	PlayerTgID  int64
	GameCount   int
	TotalNetRub int
	WinsCount   int
}
