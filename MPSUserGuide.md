# How to start MPS

1. Start MPS daemon in node  
    1.1 export CUDA_VISIBLE_DEVICES=ID 
    > Specify which GPU’s should be visible to a CUDA application.  
      
    1.2 export CUDA_MPS_PIPE_DIRECTORY=Directory
    > The MPS control daemon, the MPS server, and the associated MPS clients communicate with each other via named pipes and UNIX domain sockets.  
    The default directory for these pipes and sockets is /tmp/nvidia-mps. 
    CUDA_MPS_PIPE_DIRECTORY, can be used to override the location of these pipes and sockets.
      
    1.3 export CUDA_MPS_LOG_DIRECTORY=Directory
    > The MPS control daemon maintains a control.log file and server.log file in the directory.
      
    1.4 nvidia-smi -i ID -c EXCLUSIVE_PROCESS  
    > Three Compute Modes are supported via settings accessible in nvidia-smi.PROHIBITED ，EXCLUSIVE_PROCESS，DEFAULT.Make sure your GPU is in EXCLUSIVE_PROCESS mode.
      
    1.5 nvidia-cuda-mps-control -d 
    > start MPS daemon
 
2.  Add additional information in yaml    

    2.1 set hostIPC=true in podspec  
    > spec:  
          hostIPC: true  
      
    2.2 add environment information
    * CUDA_MPS_ACTIVE_THREAD_PERCENTAGE="number" //0-100
      > setting this in a MPS client’s environment will constraint the portion of available threads of each device.  
    * CUDA_MPS_PIPE_DIRECTORY=Directory  
      > Make sure this directory is same as what you set on node.
      
    2.3 add volume information 
    * volumeMount 
    > The same as  CUDA_MPS_PIPE_DIRECTORY set on node.
    * volumes 
    > hostPath the same as  CUDA_MPS_PIPE_DIRECTORY set on node. 
      
    2.4 A example of addtional infromation when I set CUDA_MPS_PIPE_DIRECTORY=/root/nvidia-mps
    ![example](https://ws3.sinaimg.cn/large/006tNc79ly1g4tqjrvr8rj30ou0f075w.jpg)
    
    
    