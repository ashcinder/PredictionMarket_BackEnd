package com.example.brokerfi.xc.agent.gold.viewmodel;

import android.app.Application;
import androidx.annotation.NonNull;
import androidx.lifecycle.AndroidViewModel;
import androidx.lifecycle.LiveData;
import androidx.lifecycle.MutableLiveData;

import com.example.brokerfi.xc.agent.gold.model.data.GoldMarketRepository;
import com.example.brokerfi.xc.StorageUtil;

import java.util.List;
import java.util.concurrent.atomic.AtomicBoolean;

public class GoldMyPositionsViewModel extends AndroidViewModel {
    private final GoldMarketRepository repository;
    private final MutableLiveData<List<GoldMarketRepository.GameModel>> myPositions = new MutableLiveData<>();
    private final MutableLiveData<String> error = new MutableLiveData<>();
    private final MutableLiveData<Boolean> isLoading = new MutableLiveData<>(false);
    private final MutableLiveData<String> debugToast = new MutableLiveData<>();
    private final AtomicBoolean requestInFlight = new AtomicBoolean(false);

    public GoldMyPositionsViewModel(@NonNull Application application) {
        super(application);
        String pk = StorageUtil.getCurrentPrivatekey(application);
        repository = new GoldMarketRepository(application, pk);
    }

    public LiveData<List<GoldMarketRepository.GameModel>> getMyPositions() { return myPositions; }
    public LiveData<String> getError() { return error; }
    public LiveData<Boolean> getIsLoading() { return isLoading; }
    public LiveData<String> getDebugToast() { return debugToast; }

    public void loadPositions() {
        loadPositions(true);
    }

    public void refreshPositions() {
        loadPositions(false);
    }

    private void loadPositions(boolean showLoading) {
        if (!requestInFlight.compareAndSet(false, true)) return;
        if (showLoading) isLoading.setValue(true);
        repository.getMyParticipatedGames(new GoldMarketRepository.DataCallback<List<GoldMarketRepository.GameModel>>() {
            @Override
            public void onSuccess(List<GoldMarketRepository.GameModel> models) {
                finishRequest(showLoading);
                myPositions.postValue(models);
            }
            @Override
            public void onError(String err) {
                finishRequest(showLoading);
                if (showLoading) error.postValue(err);
            }
            @Override
            public void onTiming(String source, long durationMs, boolean isFallback) {
                if (!showLoading) return;
                String msg = source + " | " + String.format(java.util.Locale.getDefault(), "%.2f秒", durationMs / 1000.0);
                if (isFallback) msg = "🔄 " + msg;
                debugToast.postValue(msg);
            }
        });
    }

    private void finishRequest(boolean showLoading) {
        requestInFlight.set(false);
        if (showLoading) isLoading.postValue(false);
    }
}
