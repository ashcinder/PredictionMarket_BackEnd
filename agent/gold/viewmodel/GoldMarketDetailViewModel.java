package com.example.brokerfi.xc.agent.gold.viewmodel;

import android.app.Application;
import androidx.annotation.NonNull;
import androidx.lifecycle.AndroidViewModel;
import androidx.lifecycle.LiveData;
import androidx.lifecycle.MutableLiveData;

import com.example.brokerfi.xc.StorageUtil;
import com.example.brokerfi.xc.agent.ai.AgentManager;
import com.example.brokerfi.xc.agent.gold.model.data.GoldMarketRepository;
import com.example.brokerfi.xc.agent.gold.model.logic.GoldAdvisoryManager;
import com.example.brokerfi.xc.agent.gold.model.logic.GoldMarketResearchPromptBuilder;

import java.math.BigInteger;
import java.util.List;

public class GoldMarketDetailViewModel extends AndroidViewModel {
    private final GoldMarketRepository repository;
    private final MutableLiveData<GoldMarketRepository.GameModel> currentGame = new MutableLiveData<>();
    private final MutableLiveData<List<GoldMarketRepository.HistoryPoint>> marketHistory = new MutableLiveData<>();
    private final MutableLiveData<String> marketAiSummary = new MutableLiveData<>();
    private final MutableLiveData<String> error = new MutableLiveData<>();
    private final MutableLiveData<Boolean> isLoading = new MutableLiveData<>(false);
    private final MutableLiveData<String> txStatus = new MutableLiveData<>();

    private String marketAiContext = "";

    public GoldMarketDetailViewModel(@NonNull Application application) {
        super(application);
        String pk = StorageUtil.getCurrentPrivatekey(application);
        repository = new GoldMarketRepository(application, pk);
    }

    public LiveData<GoldMarketRepository.GameModel> getCurrentGame() { return currentGame; }
    public LiveData<List<GoldMarketRepository.HistoryPoint>> getMarketHistory() { return marketHistory; }
    public LiveData<String> getMarketAiSummary() { return marketAiSummary; }
    public LiveData<String> getError() { return error; }
    public LiveData<Boolean> getIsLoading() { return isLoading; }
    public LiveData<String> getTxStatus() { return txStatus; }
    public String getMarketAiContext() { return marketAiContext; }

    public void loadGameInfo(int gameId) {
        isLoading.setValue(true);
        repository.getGameInfo(gameId, new GoldMarketRepository.DataCallback<GoldMarketRepository.GameModel>() {
            @Override
            public void onSuccess(GoldMarketRepository.GameModel model) {
                // 1. 查询 AI 托管状态
                repository.getAiManagedStatus(gameId, new GoldMarketRepository.DataCallback<Boolean>() {
                    @Override
                    public void onSuccess(Boolean managed) {
                        model.isManaged = managed;
                        currentGame.postValue(model);
                        
                        // 2. 从后端 MySQL 加载真实历史数据
                        loadHistory(gameId);
                        
                        isLoading.postValue(false);
                        // [优化] 移除自动请求 AI 摘要，改为手动触发
                    }
                    @Override
                    public void onError(String err) {
                        currentGame.postValue(model);
                        loadHistory(gameId);
                        isLoading.postValue(false);
                    }
                });
            }
            @Override
            public void onError(String err) {
                isLoading.postValue(false);
                error.postValue(err);
            }
        });
    }

    private void loadHistory(int gameId) {
        repository.getMarketHistory(gameId, new GoldMarketRepository.DataCallback<List<GoldMarketRepository.HistoryPoint>>() {
            @Override
            public void onSuccess(List<GoldMarketRepository.HistoryPoint> result) {
                marketHistory.postValue(result);
            }
            @Override
            public void onError(String err) {
                // 历史数据加载失败不影响主流程，仅记录
                android.util.Log.w("MarketDetailVM", "加载历史数据失败: " + err);
            }
        });
    }

    public void startAiAnalysis() {
        GoldMarketRepository.GameModel model = currentGame.getValue();
        if (model == null) return;
        
        isLoading.setValue(true);
        GoldAdvisoryManager.fetchPrice(new GoldAdvisoryManager.AdvisoryCallback() {
            @Override
            public void onSuccess(GoldAdvisoryManager.Advisory quote) {
                marketAiContext = GoldMarketResearchPromptBuilder.buildContext(model, System.currentTimeMillis(), quote);
                fetchAiSummary();
            }
            @Override
            public void onError(String err) {
                marketAiContext = GoldMarketResearchPromptBuilder.buildContext(model, System.currentTimeMillis(), null);
                fetchAiSummary();
            }
        });
    }

    public void toggleAiManaged(int gameId, boolean enabled) {
        repository.toggleAiManaged(gameId, enabled, new GoldMarketRepository.DataCallback<Boolean>() {
            @Override
            public void onSuccess(Boolean result) {
                GoldMarketRepository.GameModel model = currentGame.getValue();
                if (model != null && model.id == gameId) {
                    model.isManaged = result;
                    currentGame.postValue(model);
                }
            }
            @Override
            public void onError(String err) {
                error.postValue(err);
                // 恢复 UI 状态
                GoldMarketRepository.GameModel model = currentGame.getValue();
                if (model != null) currentGame.postValue(model);
            }
        });
    }

    private void requestAiSummary(GoldMarketRepository.GameModel model) {
        GoldAdvisoryManager.fetchPrice(new GoldAdvisoryManager.AdvisoryCallback() {
            @Override
            public void onSuccess(GoldAdvisoryManager.Advisory quote) {
                marketAiContext = GoldMarketResearchPromptBuilder.buildContext(model, System.currentTimeMillis(), quote);
                fetchAiSummary();
            }
            @Override
            public void onError(String err) {
                marketAiContext = GoldMarketResearchPromptBuilder.buildContext(model, System.currentTimeMillis(), null);
                fetchAiSummary();
            }
        });
    }

    private void fetchAiSummary() {
        AgentManager.getInstance().askGoldResearch(
                GoldMarketResearchPromptBuilder.buildSummaryPrompt(marketAiContext),
                new AgentManager.AnalysisCallback() {
                    @Override public void onBrokerReport(AgentManager.BrokerReport report) { 
                        isLoading.postValue(false);
                        marketAiSummary.postValue(report != null ? report.rawAnalysis : ""); 
                    }
                    @Override public void onGeneralAdvice(String question, String answer) { 
                        isLoading.postValue(false);
                        marketAiSummary.postValue(answer); 
                    }
                    @Override public void onError(String err) { 
                        isLoading.postValue(false);
                        error.postValue("AI error: " + err); 
                    }
                });
    }

    public void buyShares(int gameId, int optionId, BigInteger amountWei) {
        repository.buyShares(gameId, optionId, amountWei, new GoldMarketRepository.TxCallback() {
            @Override public void onTxSent(String txHash) { txStatus.postValue("Sent: " + txHash); }
            @Override public void onConfirmed(String msg) { txStatus.postValue("Confirmed: " + msg); loadGameInfo(gameId); }
            @Override public void onError(String err) { error.postValue("Failed: " + err); }
        });
    }

    public void claimReward(int gameId, int optionId) {
        repository.claimReward(gameId, optionId, new GoldMarketRepository.TxCallback() {
            @Override public void onTxSent(String txHash) { txStatus.postValue("领取交易已发送: " + txHash); }
            @Override public void onConfirmed(String msg) { txStatus.postValue("领取成功！"); loadGameInfo(gameId); }
            @Override public void onError(String err) { 
                String displayErr = err;
                if (err.contains("reverted") || err.contains("-32000")) {
                    displayErr = "领取失败：您可能不持有该博弈池的胜出份额";
                }
                error.postValue(displayErr); 
            }
        });
    }
}
