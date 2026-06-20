package com.example.brokerfi.xc.agent.gold.model.logic;

import com.example.brokerfi.xc.agent.gold.model.data.GoldMarketRepository;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * GoldGameJudge 核心裁判类
 * 职责：作为系统的“终审裁判”，解析博弈池中的 condition 字符串，并根据实时行情数据做出胜负判定。
 * 此类代替了传统 DeFi 中的预言机（Oracle），实现纯代码级的 if-else 结算。
 */
public class GoldGameJudge {

    /**
     * 根据博弈池条件进行本地 if-else 判定结算。
     * @param game 博弈池模型
     * @param currentQuote 当前行情数据
     * @return 胜出的选项索引（0 为 YES/达成，1 为 NO/未达成）
     */
    public static int evaluateGameWinner(GoldMarketRepository.GameModel game, GoldAdvisoryManager.Advisory currentQuote) {
        if (game == null || currentQuote == null) return -1;
        String cond = game.condition;
        if (cond == null || cond.isEmpty()) return -1;

        // 模版 1: 绝对价格涨跌 (Binary Price Option)
        // 格式: "博弈黄金价格在 ... 相对基准 上涨 (Price Up)"
        if (cond.contains("相对基准")) {
            if (cond.contains("上涨")) return currentQuote.change24h > 0 ? 0 : 1;
            if (cond.contains("下跌")) return currentQuote.change24h < 0 ? 0 : 1;
            if (cond.contains("持平")) return Math.abs(currentQuote.change24h) < 0.01 ? 0 : 1;
        }

        // 模版 2: 相对波动幅度 (Volatility Spread)
        // 格式: "博弈周期内波幅 >= 5% (...)"
        if (cond.contains("波幅 >= ")) {
            try {
                double threshold = parseNumericValue(cond, "波幅 >= ", "%");
                // 暂时用 change24h 模拟当日波幅，实际应获取 (High-Low)/Close
                return Math.abs(currentQuote.change24h) >= threshold ? 0 : 1;
            } catch (Exception ignored) {}
        }

        // 模版 3: 交易量控制 (Liquidity Volume)
        // 格式: "博弈指定日成交量 大于 (Above) 500 吨 (...)"
        if (cond.contains("成交量")) {
            try {
                double targetVol = parseNumericValue(cond, "成交量 ", " 吨");
                double actualVol = 400; // 这里的真实数据需对接 SGE 接口

                if (cond.contains("大于")) return actualVol > targetVol ? 0 : 1;
                if (cond.contains("小于")) return actualVol < targetVol ? 0 : 1;
                if (cond.contains("等于")) return Math.abs(actualVol - targetVol) < 1.0 ? 0 : 1;
            } catch (Exception ignored) {}
        }

        // 模版 4: 技术指标
        // 格式: "指标 RSI (14) 大于 (Above) 70 (...)"
        if (cond.contains("指标")) {
            try {
                // 复杂逻辑：此处应根据不同指标名称调用计算函数
                // 示例：简单提取数值做判定
                String valueStr = cond.substring(cond.lastIndexOf(" ") + 1, cond.indexOf(" (")).trim();
                double threshold = Double.parseDouble(valueStr);
                double currentIndicatorValue = 65.0; // 模拟当前指标值

                if (cond.contains("大于")) return currentIndicatorValue > threshold ? 0 : 1;
                if (cond.contains("小于")) return currentIndicatorValue < threshold ? 0 : 1;
            } catch (Exception ignored) {}
        }

        // 模版 5: 极值触碰 (One-Touch Barrier)
        // 格式: "金价曾触及 2500 USD (...)"
        if (cond.contains("曾触及")) {
            try {
                double target = parseNumericValue(cond, "触及 ", " USD");
                // 判定当前价是否达到或超过目标
                return currentQuote.priceUsd >= target ? 0 : 1;
            } catch (Exception ignored) {}
        }

        // 模版 6: 跨市场相对跑赢率
        // 格式: "黄金收益率跑赢 BTC (...)"
        if (cond.contains("跑赢")) {
            // 逻辑：黄金 Yield > 对标资产 Yield
            return 0; // 示例
        }

        return 1; // 默认 NO
    }

    /**
     * 辅助工具：从复杂的 condition 字符串中精准提取数值
     */
    private static double parseNumericValue(String text, String prefix, String suffix) {
        try {
            int start = text.indexOf(prefix) + prefix.length();
            int end = text.indexOf(suffix, start);
            String val = text.substring(start, end).trim();
            return Double.parseDouble(val);
        } catch (Exception e) {
            return 0;
        }
    }
}
