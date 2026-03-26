#ifndef LIBCLIENTHUB_H
#define LIBCLIENTHUB_H

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

// Callback types
typedef void (*chub_log_callback)(int level, const char* msg);
typedef void (*chub_status_callback)(int handle, int status, const char* detail);
typedef void (*chub_event_callback)(int handle, const char* eventJSON);

// Lifecycle
extern void CHubInit(void);
extern void CHubFree(void);

// Callbacks
extern void CHubSetLogCallback(chub_log_callback fn);
extern void CHubSetStatusCallback(chub_status_callback fn);
extern void CHubSetEventCallback(chub_event_callback fn);

// Client operations
extern int  CHubClientCreate(char* configJSON);
extern void CHubClientStart(int handle);
extern void CHubClientStop(int handle);
extern void CHubClientDestroy(int handle);
extern char* CHubClientGetStatus(int handle);

// Manager operations
extern int   CHubManagerCreate(char* addr, char* secret);
extern char* CHubManagerListClients(int handle);
extern char* CHubManagerListTunnels(int handle);
extern char* CHubManagerListForwards(int handle);
extern char* CHubManagerAddForward(int handle, char* paramsJSON);
extern char* CHubManagerRemoveForward(int handle, char* paramsJSON);
extern char* CHubManagerKickClient(int handle, char* name);
extern char* CHubManagerStatus(int handle);
extern char* CHubManagerListExpose(int handle, char* clientName);
extern char* CHubManagerAddExpose(int handle, char* paramsJSON);
extern char* CHubManagerRemoveExpose(int handle, char* paramsJSON);
extern void  CHubManagerDestroy(int handle);

// Memory
extern void CHubFreeString(char* s);

#ifdef __cplusplus
}
#endif

#endif
