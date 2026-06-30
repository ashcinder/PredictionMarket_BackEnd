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

public class GoldMarketDetailViewModel extends AndroidViewModel {
    private final Application application;
    private final String privateKey;
    private final GoldMarketRepository repository;
    private final MutableLiveData<GoldMarketRepository.GameModel> currentGame = new MutableLiveData<>();
    private final MutableLiveData<String> marketAiSummary = new MutableLiveData<>();
    private final MutableLiveData<String> error = new MutableLiveData<>();
    private final MutableLiveData<String> tradeError = new MutableLiveData<>();
    private final MutableLiveData<Boolean> isLoading = new MutableLiveData<>(false);
    private final MutableLiveData<String> txStatus = new MutableLiveData<>();
    private final MutableLiveData<String> debugToast = new MutableLiveData<>();

    private String marketAiContext = "";

    public GoldMarketDetailViewModel(@NonNull Application application) {
        super(application);
        this.application = application;
        this.privateKey = StorageUtil.getCurrentPrivatekey(application);
        repository = new GoldMarketRepository(application, privateKey);
    }

    private GoldMarketRepository repositoryFor(String contractAddress) {
        if (contractAddress == null || contractAddress.trim().isEmpty()) {
            return repository;
        }
        return new GoldMarketRepository(application, privateKey, contractAddress);
    }

    public LiveData<GoldMarketRepository.GameModel> getCurrentGame() { return currentGame; }
    public LiveData<String> getMarketAiSummary() { return marketAiSummary; }
    public LiveData<String> getError() { return error; }
    public LiveData<String> getTradeError() { return tradeError; }
    public LiveData<Boolean> getIsLoading() { return isLoading; }
    public LiveData<String> getTxStatus() { return txStatus; }
    public LiveData<String> getDebugToast() { return debugToast; }
    public String getMarketAiContext() { return marketAiContext; }
    public String getWalletAddress() { return repository.getWalletAddress(); }

    public void loadGameInfo(int gameId) {
        loadGameInfo(gameId, null);
    }

    public void loadGameInfo(int gameId, String contractAddress) {
        isLoading.setValue(true);
        GoldMarketRepository activeRepository = repositoryFor(contractAddress);
        activeRepository.getGameInfo(gameId, new GoldMarketRepository.DataCallback<GoldMarketRepository.GameModel>() {
            @Override
            public void onSuccess(GoldMarketRepository.GameModel model) {
                GoldMarketRepository aiRepository = repositoryFor(model != null ? model.contractAddress : contractAddress);
                aiRepository.getAiManagedStatus(gameId, new GoldMarketRepository.DataCallback<Boolean>() {
                    @Override
                    public void onSuccess(Boolean managed) {
                        model.isManaged = managed;
                        isLoading.postValue(false);
                        currentGame.postValue(model);
                    }
                    @Override
                    public void onError(String err) {
                        isLoading.postValue(false);
                        currentGame.postValue(model);
                    }
                });
            }
            @Override
            public void onError(String err) {
                isLoading.postValue(false);
                error.postValue(err);
            }
            @Override
            public void onTiming(String source, long durationMs, boolean isFallback) {
                String msg = source + " | " + String.format(java.util.Locale.getDefault(), "%.2f秒", durationMs / 1000.0);
                if (isFallback) msg = "🔄 " + msg;
                debugToast.postValue(msg);
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
        toggleAiManaged(gameId, null, enabled);
    }

    public void toggleAiManaged(int gameId, String contractAddress, boolean enabled) {
        repositoryFor(contractAddress).toggleAiManaged(gameId, enabled, new GoldMarketRepository.DataCallback<Boolean>() {
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
        buyShares(gameId, null, optionId, amountWei);
    }

    public void buyShares(int gameId, String contractAddress, int optionId, BigInteger amountWei) {
        repositoryFor(contractAddress).buyShares(gameId, optionId, amountWei, new GoldMarketRepository.TxCallback() {
            @Override public void onTxSent(String txHash) { txStatus.postValue("Sent: " + txHash); }
            @Override public void onConfirmed(String msg) {
                txStatus.postValue("Confirmed: " + msg);
                // 延迟 2 秒再刷新，确保后端 DB 同步完成 + 链上状态已更新
                new android.os.Handler(android.os.Looper.getMainLooper()).postDelayed(() -> {
                    loadGameInfo(gameId, contractAddress);
                }, 2000);
            }
            @Override public void onError(String err) { tradeError.postValue(err); }
        });
    }

    public void claimReward(int gameId, int optionId) {
        claimReward(gameId, null, optionId);
    }

    public void claimReward(int gameId, String contractAddress, int optionId) {
        repositoryFor(contractAddress).claimReward(gameId, optionId, new GoldMarketRepository.TxCallback() {
            @Override public void onTxSent(String txHash) { txStatus.postValue("Claim Sent"); }
            @Override public void onConfirmed(String msg) {
                txStatus.postValue("Claim Success");
                new android.os.Handler(android.os.Looper.getMainLooper()).postDelayed(() -> {
                    loadGameInfo(gameId, contractAddress);
                }, 2000);
            }
            @Override public void onError(String err) { tradeError.postValue("领取失败：\n\n" + err); }
        });
    }
}
