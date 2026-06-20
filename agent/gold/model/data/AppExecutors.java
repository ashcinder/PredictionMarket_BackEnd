package com.example.brokerfi.xc.agent.gold.model.data;

import android.os.Handler;
import android.os.Looper;
import java.util.concurrent.Executor;
import java.util.concurrent.Executors;

public class AppExecutors {

    private static volatile AppExecutors instance;

    private final Executor networkIO;
    private final Executor computeIO;
    private final Executor mainThread;

    private AppExecutors() {
        networkIO = Executors.newFixedThreadPool(10);
        computeIO = Executors.newSingleThreadExecutor();
        mainThread = new MainThreadExecutor();
    }

    public static AppExecutors getInstance() {
        if (instance == null) {
            synchronized (AppExecutors.class) {
                if (instance == null) instance = new AppExecutors();
            }
        }
        return instance;
    }

    public Executor networkIO() { return networkIO; }
    public Executor computeIO() { return computeIO; }
    public Executor mainThread() { return mainThread; }

    private static class MainThreadExecutor implements Executor {
        private final Handler mainThreadHandler = new Handler(Looper.getMainLooper());
        @Override
        public void execute(Runnable command) {
            mainThreadHandler.post(command);
        }
    }
}
