package com.example.brokerfi.xc.agent.ai;

public class MarketSentinel {

    public enum RiskLevel { LOW, MEDIUM, HIGH }

    public static class RiskReport {
        public RiskLevel level;
        public String message;
        public boolean shouldWarn;
    }

    public static RiskReport quickCheck(String toAddress, String value, String fee) {
        RiskReport report = new RiskReport();
        report.level = RiskLevel.LOW;
        report.shouldWarn = false;

        StringBuilder warnings = new StringBuilder();

        if (toAddress == null || toAddress.length() < 40) {
            report.level = RiskLevel.HIGH;
            report.message = "Invalid recipient address.";
            report.shouldWarn = true;
            return report;
        }

        try {
            double val = Double.parseDouble(value);
            if (val > 10000) {
                report.level = RiskLevel.MEDIUM;
                warnings.append("Large transfer (>10,000 BKC). ");
                report.shouldWarn = true;
            }
        } catch (NumberFormatException ignored) {}

        try {
            double f = Double.parseDouble(fee);
            if (f > 100) {
                report.level = RiskLevel.MEDIUM;
                warnings.append("High transaction fee (>100 BKC). ");
                report.shouldWarn = true;
            }
        } catch (NumberFormatException ignored) {}

        if (toAddress.startsWith("0x0000000000000000000000000")) {
            report.level = RiskLevel.HIGH;
            warnings.append("Sending to zero/burn address! ");
            report.shouldWarn = true;
        }

        report.message = warnings.length() > 0 ? warnings.toString().trim() : "Transaction looks safe.";
        return report;
    }

    public static void deepCheck(String toAddress, String value, String fee, SentinelCallback callback) {
        if (!DeepSeekClient.isConfigured()) {
            callback.onResult("DeepSeek not configured. Skipping AI security check.", false);
            return;
        }

        String prompt = String.format(
                "Security check: user is about to send %s BKC (fee %s) to address %s. " +
                "Does this look suspicious? Reply 'SAFE' if normal, or explain the risk in under 50 words.",
                value, fee, toAddress);

        DeepSeekClient.chatSimple(prompt, new DeepSeekClient.ChatCallback() {
            @Override
            public void onSuccess(String response) {
                boolean warn = !response.toUpperCase().contains("SAFE");
                callback.onResult(response, warn);
            }

            @Override
            public void onError(String error) {
                callback.onResult("AI check unavailable: " + error, false);
            }
        });
    }

    public interface SentinelCallback {
        void onResult(String message, boolean shouldWarn);
    }
}
