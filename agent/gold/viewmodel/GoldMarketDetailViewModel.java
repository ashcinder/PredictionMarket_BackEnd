package com.example.brokerfi.xc.agent.gold.viewmodel;

import android.app.Application;
import androidx.annotation.NonNull;
import androidx.lifecycle.AndroidViewModel;
import androidx.lifecycle.LiveData;
import androidx.lifecycle.MutableLiveData;

import com.example.brokerfi.xc.agent.gold.model.data.GoldMarketRepository;
import com.example.brokerfi.xc.agent.gold.model.logic.GoldAdvisoryManager;
import com.example.brokerfi.xc.agent.gold.model.logic.GoldMarketResearchPromptBuilder;
import com.example.brokerfi.xc.agent.model.AgentManager;
import com.example.brokerfi.xc.StorageUtil;

import java.math.BigInteger;

public class GoldMarketDetailViewModel extends AndroidViewModel {
    private final GoldMarketRepository repository;
    private final MutableLiveData<GoldMarketRepository.GameModel> currentGame = new MutableLiveData<>();
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
                isLoading.postValue(false);
                currentGame.postValue(model);
                requestAiSummary(model);
            }
            @Override
            public void onError(String err) {
                isLoading.postValue(false);
                error.postValue(err);
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
                    @Override public void onBrokerReport(AgentManager.BrokerReport report) { marketAiSummary.postValue(report != null ? report.rawAnalysis : ""); }
                    @Override public void onGeneralAdvice(String question, String answer) { marketAiSummary.postValue(answer); }
                    @Override public void onError(String err) { error.postValue("AI error: " + err); }
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
            @Override public void onTxSent(String txHash) { txStatus.postValue("Claim Sent"); }
            @Override public void onConfirmed(String msg) { txStatus.postValue("Claim Success"); loadGameInfo(gameId); }
            @Override public void onError(String err) { error.postValue("Claim Failed: " + err); }
        });
    }
}
