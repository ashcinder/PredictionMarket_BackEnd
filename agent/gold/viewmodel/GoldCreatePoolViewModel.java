package com.example.brokerfi.xc.agent.gold.viewmodel;

import android.app.Application;
import androidx.annotation.NonNull;
import androidx.lifecycle.AndroidViewModel;
import androidx.lifecycle.LiveData;
import androidx.lifecycle.MutableLiveData;

import com.example.brokerfi.xc.agent.gold.model.data.GoldMarketRepository;
import com.example.brokerfi.xc.StorageUtil;

import java.math.BigInteger;
import java.util.List;

public class GoldCreatePoolViewModel extends AndroidViewModel {
    private final GoldMarketRepository repository;
    private final MutableLiveData<String> txStatus = new MutableLiveData<>();
    private final MutableLiveData<String> error = new MutableLiveData<>();
    private final MutableLiveData<Boolean> isDeploying = new MutableLiveData<>(false);

    public GoldCreatePoolViewModel(@NonNull Application application) {
        super(application);
        String pk = StorageUtil.getCurrentPrivatekey(application);
        repository = new GoldMarketRepository(application, pk);
    }

    public LiveData<String> getTxStatus() { return txStatus; }
    public LiveData<String> getError() { return error; }
    public LiveData<Boolean> getIsDeploying() { return isDeploying; }
    public String getWalletAddress() { return repository.getWalletAddress(); }

    public void createGame(String title, String condition, byte[] imageData, String detailedInfo, List<String> optionNames, long durationSec, BigInteger liqWei) {
        isDeploying.setValue(true);
        repository.createGame(title, condition, imageData, detailedInfo, optionNames, durationSec, liqWei, new GoldMarketRepository.TxCallback() {
            @Override public void onTxSent(String hash) { txStatus.postValue("Sent: " + hash); }
            @Override public void onConfirmed(String msg) { isDeploying.postValue(false); txStatus.postValue("Success"); }
            @Override public void onError(String err) { isDeploying.postValue(false); error.postValue(err); }
        });
    }
}
