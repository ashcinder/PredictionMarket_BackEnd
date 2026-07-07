package com.example.brokerfi.xc.agent.gold.model.logic;

public final class GoldMarketStatusStyle {
    public static final int ACTIVE_TEXT = 0xFF047857;
    public static final int ACTIVE_BACKGROUND = 0xFFE7F6EF;
    public static final int PENDING_TEXT = 0xFFB45309;
    public static final int PENDING_BACKGROUND = 0xFFFFF4DE;
    public static final int RESOLVED_TEXT = 0xFF2563EB;
    public static final int RESOLVED_BACKGROUND = 0xFFEAF1FF;
    public static final int REFUNDED_TEXT = 0xFF64748B;
    public static final int REFUNDED_BACKGROUND = 0xFFF1F5F9;

    public final String label;
    public final int textColor;
    public final int backgroundColor;

    private GoldMarketStatusStyle(String label, int textColor, int backgroundColor) {
        this.label = label;
        this.textColor = textColor;
        this.backgroundColor = backgroundColor;
    }

    public static GoldMarketStatusStyle forMarket(boolean isResolved, boolean isRefunded, long remainingSeconds) {
        if (isRefunded) {
            return new GoldMarketStatusStyle("已退款", REFUNDED_TEXT, REFUNDED_BACKGROUND);
        }
        if (isResolved) {
            return new GoldMarketStatusStyle("已开奖", RESOLVED_TEXT, RESOLVED_BACKGROUND);
        }
        if (remainingSeconds <= 0) {
            return new GoldMarketStatusStyle("等待裁决", PENDING_TEXT, PENDING_BACKGROUND);
        }
        return new GoldMarketStatusStyle("运行中", ACTIVE_TEXT, ACTIVE_BACKGROUND);
    }

    private GoldMarketStatusStyle() {
        this.label = "";
        this.textColor = ACTIVE_TEXT;
        this.backgroundColor = ACTIVE_BACKGROUND;
    }
}
