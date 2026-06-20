package com.example.brokerfi.xc.agent.model;

import androidx.appcompat.app.AppCompatActivity;

import com.example.brokerfi.xc.MyUtil;
import com.example.brokerfi.xc.StorageUtil;

/**
 * Broker 收益顾问 — 监控质押收益并提供调仓建议。
 * 可被 AIAssistantActivity 或后台 Service 调用。
 */
public class BrokerAdvisor {

    public interface AdviceCallback {
        void onAdvice(String title, String detail);
        void onDataError(String error);
    }

    /**
     * 快速收益巡检：不调 AI，直接返回数据摘要。
     * 适合下拉刷新场景。
     */
    public static void quickScan(AppCompatActivity activity, AdviceCallback callback) {
        String pk = StorageUtil.getCurrentPrivatekey(activity);
        if (pk == null || pk.isEmpty()) {
            callback.onDataError("No account selected");
            return;
        }

        new Thread(() -> {
            try {
                String profitJson = MyUtil.querybrokerprofit(pk);
                if (profitJson == null || profitJson.isEmpty()) {
                    callback.onDataError("Not a broker yet. Apply first in Broker tab.");
                    return;
                }

                // 简单的本地分析（不调 AI，秒级返回）
                AgentManager.ShardProfit[] shards =
                        AgentManager.getInstance().parseShardProfitsPublic(profitJson);

                if (shards.length == 0) {
                    callback.onAdvice("Broker Status", "No active shards found.");
                    return;
                }

                StringBuilder sb = new StringBuilder();
                double bestYield = 0;
                int bestShard = -1;
                double worstYield = Double.MAX_VALUE;
                int worstShard = -1;

                for (AgentManager.ShardProfit s : shards) {
                    sb.append(String.format("Shard %d: Profit %.4f BKC | Staked %.0f BKC | Yield %.2f%%\n",
                            s.shardIndex, s.profit, s.staked, s.yieldPct * 100));
                    if (s.yieldPct > bestYield) { bestYield = s.yieldPct; bestShard = s.shardIndex; }
                    if (s.yieldPct < worstYield) { worstYield = s.yieldPct; worstShard = s.shardIndex; }
                }

                if (bestShard >= 0 && worstShard >= 0 && bestShard != worstShard) {
                    sb.append(String.format(
                            "\nSuggestion: Consider moving stake from Shard %d (%.2f%%) to Shard %d (%.2f%%).",
                            worstShard, worstYield * 100, bestShard, bestYield * 100));
                }

                callback.onAdvice("Broker Profit Scan", sb.toString());
            } catch (Exception e) {
                callback.onDataError(e.getMessage());
            }
        }).start();
    }

    /**
     * 深度 AI 分析：调 DeepSeek 做多维度研判。
     */
    public static void deepAnalysis(AppCompatActivity activity, AdviceCallback callback) {
        String pk = StorageUtil.getCurrentPrivatekey(activity);
        if (pk == null || pk.isEmpty()) {
            callback.onDataError("No account selected");
            return;
        }

        if (!DeepSeekClient.isConfigured()) {
            callback.onDataError("DeepSeek API Key not configured. Set it in AI Assistant settings.");
            return;
        }

        AgentManager.getInstance().analyzeBroker(activity, new AgentManager.AnalysisCallback() {
            @Override
            public void onBrokerReport(AgentManager.BrokerReport report) {
                StringBuilder sb = new StringBuilder(report.rawAnalysis);
                sb.append("\n\n—— Raw Data ——\n");
                for (AgentManager.ShardProfit s : report.shards) {
                    sb.append(String.format("Shard %d: profit=%.4f, staked=%.0f, yield=%.2f%%\n",
                            s.shardIndex, s.profit, s.staked, s.yieldPct * 100));
                }
                callback.onAdvice("AI Broker Analysis", sb.toString());
            }

            @Override
            public void onGeneralAdvice(String question, String answer) {
                callback.onAdvice("Analysis", answer);
            }

            @Override
            public void onError(String error) {
                callback.onDataError(error);
            }
        });
    }
}
