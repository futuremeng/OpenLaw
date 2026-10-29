// location_darwin.m 定位授权 shim。
//
// 事实依据（2026-10-29 实测 + 源码核对）：Wails v2.17 的 darwin 前端未实现任何
// CoreLocation 定位授权回调（在 wails 模块内 grep CLLocation / decidePermissionForRequest
// 均无命中）；裸二进制又没有 bundle 身份与 Info.plist 用途声明，于是 webview 里的
// navigator.geolocation 一律 PERMISSION_DENIED。这里补上最小的一块：仅在用户显式点击
// 「获取我的位置」时向系统申请 WhenInUse 授权，并把授权状态与「缺什么」如实回传。
//
// 纪律：只申请权限，不读取位置、不联网、不做任何地理编码；坐标仍由 webview 交给
// Go 侧离线判定（geo 包）。不依赖 ARC，对象为进程生命周期单例。
#import <CoreLocation/CoreLocation.h>
#import <Foundation/Foundation.h>
#import <dispatch/dispatch.h>

@interface OpenLawLocationHelper : NSObject <CLLocationManagerDelegate>
@end

// gLastStatus：delegate 在主线程更新，Go 侧轮询读取（int 读写，容忍良性竞态）。
static int gLastStatus = 0; // kCLAuthorizationStatusNotDetermined

@implementation OpenLawLocationHelper
- (void)locationManagerDidChangeAuthorization:(CLLocationManager *)manager {
    gLastStatus = (int)[manager authorizationStatus];
}
@end

static OpenLawLocationHelper *gHelper = nil;
static CLLocationManager *gManager = nil;

// 必须在主队列创建/使用 CLLocationManager：cgo 调用发生在 Go 线程，那里没有
// run loop，连接的事件源不会被调度，授权变更（delegate 回调）也就回传不到。
static void openlawEnsureManager(void) {
    if (gManager != nil) return;
    if (gHelper == nil) gHelper = [[OpenLawLocationHelper alloc] init];
    gManager = [[CLLocationManager alloc] init];
    [gManager setDelegate:gHelper];
}

// OpenLawLocationStatus 当前授权状态（尚未初始化时按「未决定」处理）。
int OpenLawLocationStatus(void) {
    __block int st = gLastStatus;
    if (gManager != nil) {
        dispatch_sync(dispatch_get_main_queue(), ^{
            st = (int)[gManager authorizationStatus];
        });
    }
    gLastStatus = st;
    return st;
}

// OpenLawRequestLocationAuthorization 在主队列发起 WhenInUse 授权申请，返回申请
// 当下的状态快照。系统弹窗是异步的，落地状态由调用方轮询 OpenLawLocationStatus。
int OpenLawRequestLocationAuthorization(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        openlawEnsureManager();
        if ([gManager authorizationStatus] == kCLAuthorizationStatusNotDetermined) {
            [gManager requestWhenInUseAuthorization];
        }
        gLastStatus = (int)[gManager authorizationStatus];
    });
    return gLastStatus;
}

// OpenLawHasLocationUsageKey Info.plist 是否声明了定位用途（缺声明时系统不会弹窗）。
int OpenLawHasLocationUsageKey(void) {
    NSDictionary *info = [NSBundle mainBundle].infoDictionary;
    if (info[@"NSLocationWhenInUseUsageDescription"] != nil) return 1;
    if (info[@"NSLocationUsageDescription"] != nil) return 1;
    return 0;
}

// OpenLawIsBundledApp 是否以 .app bundle 形态运行（裸二进制没有 TCC 身份）。
int OpenLawIsBundledApp(void) {
    return [[NSBundle mainBundle].bundlePath hasSuffix:@".app"] ? 1 : 0;
}
