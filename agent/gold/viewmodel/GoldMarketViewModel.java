package com.example.brokerfi.xc.agent.gold.viewmodel;

import android.app.Application;
import androidx.annotation.NonNull;
import androidx.lifecycle.AndroidViewModel;
import androidx.lifecycle.LiveData;
import androidx.lifecycle.MutableLiveData;

import com.example.brokerfi.xc.agent.gold.model.data.GoldMarketRepository;
import com.example.brokerfi.xc.agent.gold.model.logic.GoldAdvisoryManager;
import com.example.brokerfi.xc.StorageUtil;

import java.util.List;

public class GoldMarketViewModel extends AndroidViewModel {
    private final GoldMarketRepository repository;
    private final MutableLiveData<List<GoldMarketRepository.GameModel>> availableGames = new MutableLiveData<>();
    private final MutableLiveData<GoldAdvisoryManager.Advisory> aiAdvisory = new MutableLiveData<>();
    private final MutableLiveData<String> error = new MutableLiveData<>();
    private final MutableLiveData<Boolean> isLoading = new MutableLiveData<>(false);

    public GoldMarketViewModel(@NonNull Application application) {
        super(application);
        String pk = StorageUtil.getCurrentPrivatekey(application);
        repository = new GoldMarketRepository(application, pk);
    }

    public LiveData<List<GoldMarketRepository.GameModel>> getAvailableGames() { return availableGames; }
    public LiveData<GoldAdvisoryManager.Advisory> getAiAdvisory() { return aiAdvisory; }
    public LiveData<String> getError() { return error; }
    public LiveData<Boolean> getIsLoading() { return isLoading; }

    public void loadData() {
        if (Boolean.TRUE.equals(isLoading.getValue())) return;
        isLoading.setValue(true);
        repository.getAllGamesInfo(new GoldMarketRepository.DataCallback<List<GoldMarketRepository.GameModel>>() {
            @Override
            public void onSuccess(List<GoldMarketRepository.GameModel> models) {
                isLoading.postValue(false);
                availableGames.postValue(models);
            }
            @Override
            public void onError(String err) {
                isLoading.postValue(false);
                error.postValue(err);
            }
        });
        updateAiAdvice();
    }

    public void updateAiAdvice() {
        GoldAdvisoryManager.fetch(new GoldAdvisoryManager.AdvisoryCallback() {
            @Override public void onSuccess(GoldAdvisoryManager.Advisory advisory) { aiAdvisory.postValue(advisory); }
            @Override public void onError(String err) {}
        });
    }
}
