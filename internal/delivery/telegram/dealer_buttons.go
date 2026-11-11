package telegram

import "time"

// Кнопки djoin/drebuy/dcashout не хранятся в БД (в отличие от запросов pending_actions),
// поэтому повторное нажатие «✅ Да» отсеивается здесь. Апдейты одного чата
// обрабатываются строго по очереди (диспетчер infrastructure/telegram), так что
// второй тап, пришедший до того, как сообщение отредактировано, видит отметку первого.
//
// Отметки живут в памяти: после рестарта бота повтор снова возможен, но тогда его
// отсекают проверки сервиса (игра активна, игрок за столом / не за столом). Rebuy
// после рестарта остаётся единственным неидемпотентным случаем.

// usedButtonsTTL — сколько помнить обработанное сообщение.
const usedButtonsTTL = 24 * time.Hour

func buttonKey(c *Context) ([2]int64, bool) {
	m := c.Message()
	if m == nil {
		return [2]int64{}, false
	}
	return [2]int64{c.ChatID(), int64(m.MessageID)}, true
}

// dealerButtonUsed сообщает, что по этому сообщению дилер уже принял решение.
func (h *Handler) dealerButtonUsed(c *Context) bool {
	key, ok := buttonKey(c)
	if !ok {
		return false
	}
	h.buttonsMu.Lock()
	defer h.buttonsMu.Unlock()
	at, used := h.usedButtons[key]
	return used && time.Since(at) < usedButtonsTTL
}

// markDealerButtonUsed запоминает решение дилера по сообщению.
func (h *Handler) markDealerButtonUsed(c *Context) {
	key, ok := buttonKey(c)
	if !ok {
		return
	}
	now := time.Now()
	h.buttonsMu.Lock()
	defer h.buttonsMu.Unlock()
	for k, at := range h.usedButtons {
		if now.Sub(at) >= usedButtonsTTL {
			delete(h.usedButtons, k)
		}
	}
	h.usedButtons[key] = now
}
