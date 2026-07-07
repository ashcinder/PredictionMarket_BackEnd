package com.example.brokerfi.xc.agent.gold.model.logic;

import com.example.brokerfi.xc.agent.gold.model.data.BackendApiClient;
import com.example.brokerfi.xc.agent.gold.model.data.GoldMarketRepository;

import java.math.BigInteger;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

public final class GoldPositionHistoryPresenter {
    public static final String SNAPSHOT_TX_HASH = "__current_position_snapshot__";

    private GoldPositionHistoryPresenter() {}

    public static List<BackendApiClient.TradeDTO> visibleRows(
            GoldMarketRepository.GameModel game,
            List<BackendApiClient.TradeDTO> tradeHistory) {
        List<BackendApiClient.TradeDTO> buyTrades = new ArrayList<>();
        if (tradeHistory != null) {
            for (BackendApiClient.TradeDTO trade : tradeHistory) {
                if (trade != null && "BUY".equalsIgnoreCase(trade.tradeType)) {
                    buyTrades.add(trade);
                }
            }
        }
        if (!buyTrades.isEmpty()) {
            return buyTrades;
        }
        return snapshotRows(game);
    }

    public static boolean isSnapshotRow(BackendApiClient.TradeDTO trade) {
        return trade != null && SNAPSHOT_TX_HASH.equals(trade.txHash);
    }

    private static List<BackendApiClient.TradeDTO> snapshotRows(GoldMarketRepository.GameModel game) {
        if (game == null || game.myShares == null || game.myShares.isEmpty()) {
            return Collections.emptyList();
        }
        List<BackendApiClient.TradeDTO> rows = new ArrayList<>();
        for (int i = 0; i < game.myShares.size(); i++) {
            BigInteger shares = game.myShares.get(i);
            if (shares == null || shares.signum() <= 0) continue;

            BackendApiClient.TradeDTO row = new BackendApiClient.TradeDTO();
            row.tradeType = "BUY";
            row.optionId = i;
            row.amountWei = "";
            row.shareAmountWei = shares.toString();
            row.isSuccess = true;
            row.isAiManaged = false;
            row.txHash = SNAPSHOT_TX_HASH;
            row.createdAt = "";
            rows.add(row);
        }
        return rows;
    }
}
