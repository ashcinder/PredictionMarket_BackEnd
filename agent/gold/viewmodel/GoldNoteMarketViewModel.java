package com.example.brokerfi.xc.agent.gold.viewmodel;

import android.app.Application;
import androidx.annotation.NonNull;
import androidx.lifecycle.AndroidViewModel;
import androidx.lifecycle.LiveData;
import androidx.lifecycle.MutableLiveData;

import com.example.brokerfi.xc.agent.gold.model.logic.GoldAdvisoryManager;

public class GoldNoteMarketViewModel extends AndroidViewModel {
    private final MutableLiveData<GoldAdvisoryManager.Advisory> quote = new MutableLiveData<>();
    private final MutableLiveData<String> error = new MutableLiveData<>();

    public GoldNoteMarketViewModel(@NonNull Application application) { super(application); }

    public LiveData<GoldAdvisoryManager.Advisory> getQuote() { return quote; }
    public LiveData<String> getError() { return error; }

    public void loadPrice() {
        GoldAdvisoryManager.fetchPrice(new GoldAdvisoryManager.AdvisoryCallback() {
            @Override public void onSuccess(GoldAdvisoryManager.Advisory q) { quote.postValue(q); }
            @Override public void onError(String err) { error.postValue(err); }
        });
    }
}
